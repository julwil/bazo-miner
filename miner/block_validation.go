package miner

import (
	"errors"
	"fmt"
	"github.com/julwil/bazo-miner/crypto"
	"github.com/julwil/bazo-miner/p2p"
	"github.com/julwil/bazo-miner/protocol"
	"github.com/julwil/bazo-miner/storage"
	"strconv"
	"time"
)

// This function is split into block syntax/PoS check and actual state change
// because there is the case that we might need to go fetch several blocks
// and have to check the blocks first before changing the state in the correct order.
func validate(b *protocol.Block, initialSetup bool) error {

	//This mutex is necessary that own-mined blocks and received blocks from the network are not
	//validated concurrently.
	blockValidation.Lock()
	defer blockValidation.Unlock()

	if storage.ReadClosedBlock(b.Hash) != nil {
		logger.Printf("Received block (%x) has already been validated.\n", b.Hash[0:8])
		return errors.New("Received Block has already been validated.")
	}

	//Get the right branch, and a list of blocks to rollback (if necessary).
	blocksToRollback, blocksToValidate, err := getBlockSequences(b)
	if err != nil {
		return err
	}

	if len(blocksToRollback) > 0 {
		logger.Printf(" _____________________")
		logger.Printf("| Blocks To Rollback: |________________________________________________")
		for _, block := range blocksToRollback {
			logger.Printf("|  - %x  |", block.Hash)
		}
		logger.Printf("|______________________________________________________________________|")
		logger.Printf(" _____________________")
		logger.Printf("| Blocks To Validate: |________________________________________________")
		for _, block := range blocksToValidate {
			logger.Printf("|  - %x  |", block.Hash)
		}
		logger.Printf("|______________________________________________________________________|")
	}

	//Verify block time is dynamic and corresponds to system time at the time of retrieval.
	//If we are syncing or far behind, we cannot do this dynamic check,
	//therefore we include a boolean uptodate. If it's true we consider ourselves uptodate and
	//do dynamic time checking.
	uptodate = len(blocksToValidate) < DELAYED_BLOCKS

	//Prepare datastructure to fill tx payloads.
	blockDataMap := make(map[[32]byte]blockData)

	// First perform rollback (if necessary)
	for _, block := range blocksToRollback {
		if err := rollback(block); err != nil {
			return err
		}
	}

	// Secondly, perform the validation.
	for _, block := range blocksToValidate {

		// We need to allocate slice space for the underlying array when we pass them as reference.
		accTxs := make([]*protocol.AccTx, block.NrAccTx)
		fundsTxs := make([]*protocol.FundsTx, block.NrFundsTx) // TODO: Duplicate?
		configTxs := make([]*protocol.ConfigTx, block.NrConfigTx)
		stakeTxs := make([]*protocol.StakeTx, block.NrStakeTx)
		aggTxs := make([]*protocol.AggTx, block.NrAggTx)
		deleteTxs := make([]*protocol.DeleteTx, block.NrDeleteTx)
		var aggregatedFundsTxs []*protocol.FundsTx // TODO: Duplicate?

		err = preValidate(block, initialSetup)
		if err != nil {
			return err
		}

		// Fetching payload data from the txs (if necessary, ask other miners).
		err = fetchTransactions(
			initialSetup,
			block,
			&accTxs,
			&fundsTxs,
			&configTxs,
			&stakeTxs,
			&aggTxs,
			&aggregatedFundsTxs,
			&deleteTxs,
		)
		if err != nil {
			return err
		}

		//Check if the validator that added the block has previously voted on different competing chains (find slashing proof).
		//The proof will be stored in the global slashing dictionary.
		if block.Height > 0 {
			seekSlashingProof(block)
		}

		if err != nil {
			return err
		}

		blockDataMap[block.Hash] = blockData{
			accTxs,
			fundsTxs,
			configTxs,
			stakeTxs,
			aggTxs,
			aggregatedFundsTxs,
			deleteTxs,
			block,
		}
		if err := validateState(blockDataMap[block.Hash], initialSetup); err != nil {
			return err
		}

		postValidate(blockDataMap[block.Hash], initialSetup)

		logString := "After rollback for"
		if len(blocksToRollback) == 0 {
			logString = "During Validation of other"
		}

		logger.Printf("Validated block (%s block %v): %vState:\n%s", logString, b.Hash[0:8], block, getState())
	}

	return nil
}

//Doesn't involve any state changes.
func preValidate(block *protocol.Block, initialSetup bool) error {

	//Check state contains beneficiary.
	beneficiaryAcc, err := storage.GetAccount(block.Beneficiary)
	if err != nil {
		return err
	}

	// Check if node is part of the validator set.
	if !beneficiaryAcc.IsStaking {
		return errors.New("Validator is not part of the validator set.")
	}

	//This dynamic check is only done if we're up-to-date with syncing, otherwise timestamp is not checked.
	//Other miners (which are up-to-date) made sure that this is correct.
	if !initialSetup && uptodate {
		if err := timestampCheck(block.Timestamp); err != nil {
			return err
		}
	}

	//Check block size.
	if block.GetSize() > activeParameters.BlockSize {
		return errors.New("Block size too large.")
	}

	err = validateNoDuplicateTx(block)
	if err != nil {
		return err
	}

	//First, initialize an RSA Public Key instance with the modulus of the proposer of the block (beneficiaryAcc)
	//Second, check if the commitment proof of the proposed block can be verified with the public key
	//Invalid if the commitment proof can not be verified with the public key of the proposer
	commitmentPubKey, err := crypto.CreateRSAPubKeyFromBytes(beneficiaryAcc.CommitmentKey)
	if err != nil {
		return errors.New("Invalid commitment key in account.")
	}

	err = crypto.VerifyMessageWithRSAKey(commitmentPubKey, fmt.Sprint(block.Height), block.CommitmentProof)
	if err != nil {
		return errors.New("The submitted commitment proof can not be verified.")
	}

	//Invalid if PoS calculation is not correct.
	prevProofs := GetLatestProofs(activeParameters.numIncludedPrevProofs, block)

	//PoS validation
	if !initialSetup && !validateProofOfStake(getDifficulty(), prevProofs, block.Height, beneficiaryAcc.Balance, block.CommitmentProof, block.Timestamp) {
		logger.Printf("____________________NONCE (%x) in block %x is problematic", block.Nonce, block.Hash[0:8])
		logger.Printf("|  block.Height: %d, beneficiaryAcc.Address %x, beneficiaryAcc.txCount %v, beneficiaryAcc.Balance %v, block.CommitmentProf: %x, block.Timestamp %v ", block.Height, beneficiaryAcc.Address[0:8], beneficiaryAcc.TxCnt, beneficiaryAcc.Balance, block.CommitmentProof[0:8], block.Timestamp)
		logger.Printf("|_____________________________________________________")

		return errors.New("The nonce is incorrect.")
	}

	//Invalid if PoS is too far in the future.
	now := time.Now()
	if block.Timestamp > now.Unix()+int64(activeParameters.AcceptedTimeDiff) {
		return errors.New("The timestamp is too far in the future. " + string(block.Timestamp) + " vs " + string(now.Unix()))
	}

	//Check for minimum waiting time.
	if block.Height-beneficiaryAcc.StakingBlockHeight < uint32(activeParameters.WaitingMinimum) {
		return errors.New(
			"The miner must wait a minimum amount of blocks before start validating. " +
				"Block Height:" + fmt.Sprint(block.Height) + " - Height when started validating " +
				string(beneficiaryAcc.StakingBlockHeight) + " MinWaitingTime: " + string(activeParameters.WaitingMinimum))
	}

	//Check if block contains a proof for two conflicting block hashes, else no proof provided.
	if block.SlashedAddress != [32]byte{} {
		if _, err = slashingCheck(
			block.SlashedAddress,
			block.ConflictingBlockHash1,
			block.ConflictingBlockHash2,
			block.ConflictingBlockHashWithoutTx1,
			block.ConflictingBlockHashWithoutTx2); err != nil {
			return err
		}
	}

	sha3Hash := block.Sha3Hash()
	hashInput := sha3Hash[:]
	chamHash := crypto.ChameleonHash(block.ChamHashParameters, block.ChamHashCheckString, &hashInput)

	if block.Aggregated == false && chamHash != block.Hash {
		return errors.New("Merkle Root is incorrect.")
	}

	return nil
}

//Dynamic state check.
func validateState(data blockData, initialSetup bool) error {
	//The sequence of validation matters. If we start with accs, then fund/stake transactions can be done in the same block
	//even though the accounts did not exist before the block validation.

	if err := accStateChange(data.accTxSlice); err != nil {
		return err
	}

	if err := fundsStateChange(data.fundsTxSlice, initialSetup); err != nil {
		accStateChangeRollback(data.accTxSlice)
		return err
	}

	if err := aggTxStateChange(data.aggregatedFundsTxSlice, initialSetup); err != nil {
		fundsStateChangeRollback(data.fundsTxSlice)
		accStateChangeRollback(data.accTxSlice)
		return err
	}

	if err := stakeStateChange(data.stakeTxSlice, data.block.Height, initialSetup); err != nil {
		fundsStateChangeRollback(data.fundsTxSlice)
		accStateChangeRollback(data.accTxSlice)
		aggregatedStateRollback(data.aggTxSlice, data.block.HashWithoutTx, data.block.Beneficiary)
		return err
	}

	if err := collectTxFees(data.accTxSlice, data.fundsTxSlice, data.configTxSlice, data.stakeTxSlice, data.aggTxSlice, data.block.Beneficiary, initialSetup); err != nil {
		stakeStateChangeRollback(data.stakeTxSlice)
		fundsStateChangeRollback(data.fundsTxSlice)
		aggregatedStateRollback(data.aggTxSlice, data.block.HashWithoutTx, data.block.Beneficiary)
		accStateChangeRollback(data.accTxSlice)
		return err
	}

	if err := collectBlockReward(activeParameters.BlockReward, data.block.Beneficiary, initialSetup); err != nil {
		collectTxFeesRollback(data.accTxSlice, data.fundsTxSlice, data.configTxSlice, data.stakeTxSlice, data.block.Beneficiary)
		stakeStateChangeRollback(data.stakeTxSlice)
		fundsStateChangeRollback(data.fundsTxSlice)
		aggregatedStateRollback(data.aggTxSlice, data.block.HashWithoutTx, data.block.Beneficiary)
		accStateChangeRollback(data.accTxSlice)
		return err
	}

	if err := collectSlashReward(activeParameters.SlashReward, data.block); err != nil {
		collectBlockRewardRollback(activeParameters.BlockReward, data.block.Beneficiary)
		collectTxFeesRollback(data.accTxSlice, data.fundsTxSlice, data.configTxSlice, data.stakeTxSlice, data.block.Beneficiary)
		stakeStateChangeRollback(data.stakeTxSlice)
		fundsStateChangeRollback(data.fundsTxSlice)
		aggregatedStateRollback(data.aggTxSlice, data.block.HashWithoutTx, data.block.Beneficiary)
		accStateChangeRollback(data.accTxSlice)
		return err
	}

	if err := updateStakingHeight(data.block); err != nil {
		collectSlashRewardRollback(activeParameters.SlashReward, data.block)
		collectBlockRewardRollback(activeParameters.BlockReward, data.block.Beneficiary)
		collectTxFeesRollback(data.accTxSlice, data.fundsTxSlice, data.configTxSlice, data.stakeTxSlice, data.block.Beneficiary)
		stakeStateChangeRollback(data.stakeTxSlice)
		fundsStateChangeRollback(data.fundsTxSlice)
		aggregatedStateRollback(data.aggTxSlice, data.block.HashWithoutTx, data.block.Beneficiary)
		accStateChangeRollback(data.accTxSlice)
		return err
	}

	return nil
}

func postValidate(data blockData, initialSetup bool) {

	//The new system parameters get active if the block was successfully validated
	//This is done after state validation (in contrast to accTx/fundsTx).
	//Conversely, if blocks are rolled back, the system parameters are changed first.
	configStateChange(data.configTxSlice, data.block.Hash)
	//Collects meta information about the block (and handled difficulty adaption).
	collectStatistics(data.block)

	//When starting a miner there are various scenarios how to PostValidate a block
	// 1. Bootstrapping Miner on InitialSetup 		--> All Tx Are already in closedBucket
	// 2. Bootstrapping Miner after InitialSetup	--> PostValidate normal, writing tx into closed bucket.
	// 3. Normal Miner on InitialSetup 				-->	Write All Tx Into Closed Tx
	// 4. Normal Miner after InitialSetup			-->	Write All Tx Into Closed Tx
	if !p2p.IsBootstrap() || !initialSetup {
		//Write all open transactions to closed/validated storage.
		for _, tx := range data.accTxSlice {
			storage.WriteClosedTx(tx)
			storage.DeleteOpenTx(tx)
		}

		for _, tx := range data.fundsTxSlice {
			storage.WriteClosedTx(tx)
			tx.Block = data.block.HashWithoutTx
			storage.DeleteOpenTx(tx)
			storage.DeleteINVALIDOpenTx(tx)
		}

		for _, tx := range data.configTxSlice {
			storage.WriteClosedTx(tx)
			storage.DeleteOpenTx(tx)
		}

		for _, tx := range data.stakeTxSlice {
			storage.WriteClosedTx(tx)
			storage.DeleteOpenTx(tx)
		}

		for _, tx := range data.deleteTxSlice {
			storage.WriteClosedTx(tx)
			storage.DeleteOpenTx(tx)
		}

		//Store all recursively fetched funds transactions.
		if initialSetup {
			for _, tx := range data.aggregatedFundsTxSlice {
				tx.Aggregated = true
				storage.WriteClosedTx(tx)
				storage.DeleteOpenTx(tx)
			}
		}

		for _, tx := range data.aggTxSlice {

			//delete FundsTx per aggTx in open storage and write them to the closed storage.
			for _, aggregatedTxHash := range tx.AggregatedTxSlice {
				trx := storage.ReadClosedTx(aggregatedTxHash)
				if trx != nil {
					switch trx.(type) {
					case *protocol.AggTx:
						trx.(*protocol.AggTx).Aggregated = true
					case *protocol.FundsTx:
						trx.(*protocol.FundsTx).Aggregated = true
					}
				} else {
					trx = storage.ReadOpenTx(aggregatedTxHash)
					if trx == nil {
						for _, i := range data.aggregatedFundsTxSlice {
							if i.Hash() == aggregatedTxHash {
								trx = i
							}
						}
					}
					switch trx.(type) {
					case *protocol.AggTx:
						trx.(*protocol.AggTx).Aggregated = true
					case *protocol.FundsTx:
						trx.(*protocol.FundsTx).Block = data.block.HashWithoutTx
						trx.(*protocol.FundsTx).Aggregated = true
					}
				}
				if trx == nil {
					break
				}

				storage.WriteClosedTx(trx)
				storage.DeleteOpenTx(trx)
				storage.DeleteINVALIDOpenTx(tx)
			}

			//Delete AggTx and write it to closed Tx.
			tx.Block = data.block.HashWithoutTx
			tx.Aggregated = false
			storage.WriteClosedTx(tx)
			storage.DeleteOpenTx(tx)
			storage.DeleteINVALIDOpenTx(tx)
		}

		if len(data.fundsTxSlice) > 0 {
			broadcastVerifiedFundsTxs(data.fundsTxSlice)
			//Current sending mechanism is not  fast enough to broadcast all validated transactions...
			//broadcastVerifiedFundsTxsToOtherMiners(data.fundsTxSlice)
			//broadcastVerifiedFundsTxsToOtherMiners(data.aggregatedFundsTxSlice)
		}

		//Broadcast AggTx to the neighbors, such that they do not have to request them later.
		if len(data.aggTxSlice) > 0 {
			//broadcastVerifiedAggTxsToOtherMiners(data.aggTxSlice)
		}

		//It might be that block is not in the openblock storage, but this doesn't matter.
		storage.DeleteOpenBlock(data.block.Hash)
		storage.WriteClosedBlock(data.block)

		//Do not empty last three blocks and only if it not aggregated already.
		for _, block := range storage.ReadAllClosedBlocks() {

			//Empty all blocks despite the last NO_AGGREGATION_LENGTH and genesis block.
			if !block.Aggregated && block.Height > 0 {
				if (int(block.Height)) < (int(data.block.Height) - NO_EMPTYING_LENGTH) {
					storage.UpdateBlocksToBlocksWithoutTx(block)
				}
			}
		}

		// Write last block to db and delete last block's ancestor.
		storage.DeleteAllLastClosedBlock()
		storage.WriteLastClosedBlock(data.block)
	}
}

//Only blocks with timestamp not diverging from system time (past or future) more than one hour are accepted.
func timestampCheck(timestamp int64) error {
	systemTime := p2p.ReadSystemTime()

	if timestamp > systemTime {
		if timestamp-systemTime > int64(2*time.Hour.Seconds()) {
			return errors.New("Timestamp was too far in the future.System time: " + strconv.FormatInt(systemTime, 10) + " vs. timestamp " + strconv.FormatInt(timestamp, 10) + "\n")
		}
	} else {
		if systemTime-timestamp > int64(10*time.Hour.Seconds()) {
			return errors.New("Timestamp was too far in the past. System time: " + strconv.FormatInt(systemTime, 10) + " vs. timestamp " + strconv.FormatInt(timestamp, 10) + "\n")
		}
	}

	return nil
}

func slashingCheck(slashedAddress, conflictingBlockHash1, conflictingBlockHash2, conflictingBlockHashWithoutTx1, conflictingBlockHashWithoutTx2 [32]byte) (bool, error) {
	prefix := "Invalid slashing proof: "

	if conflictingBlockHash1 == [32]byte{} || conflictingBlockHash2 == [32]byte{} {
		return false, errors.New(fmt.Sprintf(prefix + "Invalid conflicting block hashes provided."))
	}

	if conflictingBlockHash1 == conflictingBlockHash2 {
		return false, errors.New(fmt.Sprintf(prefix + "Conflicting block hashes are the same."))
	}

	//Fetch the blocks for the provided block hashes.
	conflictingBlock1 := storage.ReadClosedBlock(conflictingBlockHash1)
	conflictingBlock2 := storage.ReadClosedBlock(conflictingBlockHash2)

	//Try fetching the block from the Blocks Without Transactions.
	if conflictingBlock1 == nil {
		conflictingBlock1 = storage.ReadClosedBlockWithoutTx(conflictingBlockHashWithoutTx1)
	}
	if conflictingBlock2 == nil {
		conflictingBlock2 = storage.ReadClosedBlockWithoutTx(conflictingBlockHashWithoutTx2)
	}

	if IsInSameChain(conflictingBlock1, conflictingBlock2) {
		return false, errors.New(fmt.Sprintf(prefix + "Conflicting block hashes are on the same chain."))
	}

	//TODO Optimize code (duplicated)
	//If this block is unknown we need to check if its in the openblock storage or we must request it.
	if conflictingBlock1 == nil {
		conflictingBlock1 = storage.ReadOpenBlock(conflictingBlockHash1)
		if conflictingBlock1 == nil {
			//Fetch the block we apparently missed from the network.
			p2p.BlockReq(conflictingBlockHash1, conflictingBlockHashWithoutTx1)

			//Blocking wait
			select {
			case encodedBlock := <-p2p.BlockReqChan:
				conflictingBlock1 = conflictingBlock1.Decode(encodedBlock)
				//Limit waiting time to BLOCKFETCH_TIMEOUT seconds before aborting.
			case <-time.After(BLOCKFETCH_TIMEOUT * time.Second):
				if p2p.BlockAlreadyReceived(storage.ReadReceivedBlockStash(), conflictingBlockHash1) {
					for _, block := range storage.ReadReceivedBlockStash() {
						if block.Hash == conflictingBlockHash1 {
							conflictingBlock1 = block
							break
						}
					}
					logger.Printf("Block %x received Before", conflictingBlockHash1)
					break
				}
				return false, errors.New(fmt.Sprintf(prefix + "Could not find a block with the provided conflicting hash (1)."))
			}
		}

		ancestor, _ := getNewChain(conflictingBlock1)
		if ancestor == nil {
			return false, errors.New(fmt.Sprintf(prefix + "Could not find a ancestor for the provided conflicting hash (1)."))
		}
	}

	//TODO Optimize code (duplicated)
	//If this block is unknown we need to check if its in the openblock storage or we must request it.
	if conflictingBlock2 == nil {
		conflictingBlock2 = storage.ReadOpenBlock(conflictingBlockHash2)
		if conflictingBlock2 == nil {
			//Fetch the block we apparently missed from the network.
			p2p.BlockReq(conflictingBlockHash2, conflictingBlockHashWithoutTx2)

			//Blocking wait
			select {
			case encodedBlock := <-p2p.BlockReqChan:
				conflictingBlock2 = conflictingBlock2.Decode(encodedBlock)
				//Limit waiting time to BLOCKFETCH_TIMEOUT seconds before aborting.
			case <-time.After(BLOCKFETCH_TIMEOUT * time.Second):
				if p2p.BlockAlreadyReceived(storage.ReadReceivedBlockStash(), conflictingBlockHash2) {
					for _, block := range storage.ReadReceivedBlockStash() {
						if block.Hash == conflictingBlockHash2 {
							conflictingBlock2 = block
							break
						}
					}
					logger.Printf("Block %x received Before", conflictingBlockHash2)
					break
				}
				return false, errors.New(fmt.Sprintf(prefix + "Could not find a block with the provided conflicting hash (2)."))
			}
		}

		ancestor, _ := getNewChain(conflictingBlock2)
		if ancestor == nil {
			return false, errors.New(fmt.Sprintf(prefix + "Could not find a ancestor for the provided conflicting hash (2)."))
		}
	}

	// We found the height of the blocks and the height of the blocks can be checked.
	// If the height is not within the active slashing window size, we must throw an error. If not, the proof is valid.
	if !(conflictingBlock1.Height < uint32(activeParameters.SlashingWindowSize)+conflictingBlock2.Height) {
		return false, errors.New(fmt.Sprintf(prefix + "Could not find a ancestor for the provided conflicting hash (2)."))
	}

	//Delete the proof from local slashing dictionary. If proof has not existed yet, nothing will be deleted.
	delete(slashingDict, slashedAddress)

	return true, nil
}

// Validate whether the given block doesn't contain duplicate txs.
// Duplicate txs are not allowed and result in a failed validation.
func validateNoDuplicateTx(block *protocol.Block) error {
	seenTxs := make(map[[32]byte]bool)
	for _, txHash := range block.AccTxData {
		if _, exists := seenTxs[txHash]; exists {
			return errors.New("Duplicate Account Transaction Hash detected.")
		}
		seenTxs[txHash] = true
	}
	for _, txHash := range block.FundsTxData {
		if _, exists := seenTxs[txHash]; exists {
			return errors.New("Duplicate Funds Transaction Hash detected.")
		}
		seenTxs[txHash] = true
	}
	for _, txHash := range block.ConfigTxData {
		if _, exists := seenTxs[txHash]; exists {
			return errors.New("Duplicate Config Transaction Hash detected.")
		}
		seenTxs[txHash] = true
	}
	for _, txHash := range block.StakeTxData {
		if _, exists := seenTxs[txHash]; exists {
			return errors.New("Duplicate Stake Transaction Hash detected.")
		}
		seenTxs[txHash] = true
	}
	for _, txHash := range block.AggTxData {
		if _, exists := seenTxs[txHash]; exists {
			return errors.New("Duplicate Aggregation Transaction Hash detected.")
		}
		seenTxs[txHash] = true
	}
	for _, txHash := range block.DeleteTxData {
		if _, exists := seenTxs[txHash]; exists {
			return errors.New("Duplicate Delete Transaction Hash detected.")
		}
		seenTxs[txHash] = true
	}

	return nil
}
