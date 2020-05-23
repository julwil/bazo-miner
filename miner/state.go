package miner

import (
	"errors"
	"fmt"
	"github.com/julwil/bazo-miner/crypto"
	"github.com/julwil/bazo-miner/p2p"
	"github.com/julwil/bazo-miner/protocol"
	"github.com/julwil/bazo-miner/storage"
	"sort"
	"strconv"
	"time"
)

//Separate function to reuse mechanism in client implementation
func CheckAndChangeParameters(parameters *Parameters, configTxSlice *[]*protocol.ConfigTx) (change bool) {
	for _, tx := range *configTxSlice {
		switch tx.Id {
		case protocol.FEE_MINIMUM_ID:
			if parameterBoundsChecking(protocol.FEE_MINIMUM_ID, tx.Payload) {
				parameters.FeeMinimum = tx.Payload
				change = true
			}
		case protocol.BLOCK_SIZE_ID:
			if parameterBoundsChecking(protocol.BLOCK_SIZE_ID, tx.Payload) {
				parameters.BlockSize = tx.Payload
				logger.Printf("BLOCK_SIZE: %v", parameters.BlockSize)
				change = true
			}
		case protocol.BLOCK_REWARD_ID:
			if parameterBoundsChecking(protocol.BLOCK_REWARD_ID, tx.Payload) {
				parameters.BlockReward = tx.Payload
				change = true
			}
		case protocol.DIFF_INTERVAL_ID:
			if parameterBoundsChecking(protocol.DIFF_INTERVAL_ID, tx.Payload) {
				parameters.DiffInterval = tx.Payload
				logger.Printf("BLOCK_DIFF: %v", parameters.DiffInterval)
				change = true
			}
		case protocol.BLOCK_INTERVAL_ID:
			if parameterBoundsChecking(protocol.BLOCK_INTERVAL_ID, tx.Payload) {
				parameters.BlockInterval = tx.Payload
				logger.Printf("BLOCK_INVTERVAL: %v", parameters.BlockInterval)
				change = true
			}
		case protocol.STAKING_MINIMUM_ID:
			if parameterBoundsChecking(protocol.STAKING_MINIMUM_ID, tx.Payload) {
				parameters.StakingMinimum = tx.Payload
				change = true
				//Go through all accounts and remove all validators from the validator sett that no longer fulfill the minimum staking amount
				for _, account := range storage.State {
					if account.IsStaking && account.Balance < 0+tx.Payload {
						account.IsStaking = false
					}
				}
			}
		case protocol.WAITING_MINIMUM_ID:
			if parameterBoundsChecking(protocol.WAITING_MINIMUM_ID, tx.Payload) {
				parameters.WaitingMinimum = tx.Payload
				change = true
			}
		case protocol.ACCEPTANCE_TIME_DIFF_ID:
			if parameterBoundsChecking(protocol.ACCEPTANCE_TIME_DIFF_ID, tx.Payload) {
				parameters.AcceptedTimeDiff = tx.Payload
				change = true
			}
		case protocol.SLASHING_WINDOW_SIZE_ID:
			if parameterBoundsChecking(protocol.SLASHING_WINDOW_SIZE_ID, tx.Payload) {
				parameters.SlashingWindowSize = tx.Payload
				change = true
			}
		case protocol.SLASHING_REWARD_ID:
			if parameterBoundsChecking(protocol.SLASHING_REWARD_ID, tx.Payload) {
				parameters.SlashReward = tx.Payload
				change = true
			}
		}
	}

	return change
}

//For logging purposes
func getState() (state string) {
	for _, acc := range storage.State {
		state += fmt.Sprintf("Is root: %v, %v\n", storage.IsRootKey(acc.Hash()), acc)
	}
	return state
}

func initState() (initialBlock *protocol.Block, err error) {
	var allClosedBlocks []*protocol.Block
	if p2p.IsBootstrap() {
		allClosedBlocks = storage.ReadAllClosedBlocks()
	} else {
		p2p.LastBlockReq()
		var lastBlock *protocol.Block
		//Blocking wait
		select {
		case encodedBlock := <-p2p.BlockReqChan:
			lastBlock = lastBlock.Decode(encodedBlock)
			//Limit waiting time to BLOCKFETCH_TIMEOUT seconds before aborting.
		case <-time.After(BLOCKFETCH_TIMEOUT * time.Second):
			return nil, errors.New(fmt.Sprintf("Timeout requesting last block for initial startup..."))
		}

		storage.WriteClosedBlock(lastBlock)
		storage.WriteLastClosedBlock(lastBlock)
		if len(allClosedBlocks) > 0 && allClosedBlocks[len(allClosedBlocks)-1].Hash == lastBlock.Hash {
			fmt.Printf("Block with height %v already exists", lastBlock.Height)
		} else {
			allClosedBlocks = append(allClosedBlocks, lastBlock)
		}

		for {
		RETRY:
			p2p.BlockReq(lastBlock.PrevHash, lastBlock.PrevHashWithoutTx)
			//p2p.BlockReq(lastBlock.PrevHash, lastBlock.PrevHashWithoutTx)
			select {
			case encodedBlock := <-p2p.BlockReqChan:
				lastBlock = lastBlock.Decode(encodedBlock)
				//Limit waiting time to BLOCKFETCH_TIMEOUT seconds before aborting.
			case <-time.After(BLOCKFETCH_TIMEOUT * time.Second):
				if p2p.BlockAlreadyReceived(storage.ReadReceivedBlockStash(), lastBlock.PrevHash) {
					for _, block := range storage.ReadReceivedBlockStash() {
						if block.Hash == lastBlock.PrevHash {
							lastBlock = block
							break
						}
					}
					logger.Printf("Block %x received Before", lastBlock.PrevHash[0:8])
					break
				} else {
					logger.Printf("Timed out while requesting %x", lastBlock.PrevHash[0:8])
					goto RETRY
				}
			}

			//write aggregated blocks to the 'closedblockswithouttx' bucket. Else to the normal closedblocks bucket.
			if lastBlock.Aggregated == true {
				storage.WriteClosedBlockWithoutTx(lastBlock)
			} else {
				storage.WriteClosedBlock(lastBlock)
			}

			if len(allClosedBlocks) > 0 && allClosedBlocks[len(allClosedBlocks)-1].Hash == lastBlock.Hash {
				fmt.Printf("Block with height %v already exists", lastBlock.Height)
			} else {
				allClosedBlocks = append(allClosedBlocks, lastBlock)
			}
			//fmt.Println("Last block: ", lastBlock.Height)
			if lastBlock.Height == 0 {
				break
			}
		}
	}

	if len(allClosedBlocks) > 0 {
		//Set the last closed block as the initial block
		initialBlock = allClosedBlocks[0]
		for _, blockToValidate := range allClosedBlocks {
			if blockToValidate.Height > initialBlock.Height {
				initialBlock = blockToValidate
			}
		}
	} else {
		initialBlock = newBlock([32]byte{}, [32]byte{}, [crypto.COMM_PROOF_LENGTH]byte{}, 0)

		commitmentProof, err := crypto.SignMessageWithRSAKey(rootCommPrivKey, fmt.Sprint(initialBlock.Height))
		if err != nil {
			return nil, err
		}
		copy(initialBlock.CommitmentProof[:], commitmentProof[:])

		//Append genesis block to the map and save in storage
		allClosedBlocks = append(allClosedBlocks, initialBlock)

		storage.WriteLastClosedBlock(initialBlock)
		storage.WriteClosedBlock(initialBlock)
	}

	if !p2p.IsBootstrap() {
		allClosedBlocks = InvertBlockArray(allClosedBlocks)
	}

	//Validate all closed blocks and update state
	for _, blockToValidate := range allClosedBlocks {
		//Prepare datastructure to fill tx payloads
		blockDataMap := make(map[[32]byte]blockData)

		//Do not validate the genesis block, since a lot of properties are set to nil
		if blockToValidate.Hash != [32]byte{} {
			// We need to allocate slice space for the underlying array when we pass them as reference.
			accTxs := make([]*protocol.AccTx, blockToValidate.NrAccTx)
			fundsTxs := make([]*protocol.FundsTx, blockToValidate.NrFundsTx) // TODO: Duplicate?
			configTxs := make([]*protocol.ConfigTx, blockToValidate.NrConfigTx)
			stakeTxs := make([]*protocol.StakeTx, blockToValidate.NrStakeTx)
			aggTxs := make([]*protocol.AggTx, blockToValidate.NrAggTx)
			deleteTxs := make([]*protocol.DeleteTx, blockToValidate.NrDeleteTx)
			var aggregatedFundsTxs []*protocol.FundsTx // TODO: Duplicate?

			err = preValidate(blockToValidate, true)
			if err != nil {
				return nil, errors.New(fmt.Sprintf("Block (%x) could not be prevalidated: %v\n", blockToValidate.Hash[0:8], err))
			}

			err = fetchTransactions(
				true,
				blockToValidate,
				&accTxs,
				&fundsTxs,
				&configTxs,
				&stakeTxs,
				&aggTxs,
				&aggregatedFundsTxs,
				&deleteTxs,
			)
			if err != nil {
				return nil, errors.New(fmt.Sprintf("Block (%x) could not be prevalidated: %v\n", blockToValidate.Hash[0:8], err))
			}

			blockDataMap[blockToValidate.Hash] = blockData{
				accTxs,
				fundsTxs,
				configTxs,
				stakeTxs,
				aggTxs,
				aggregatedFundsTxs,
				deleteTxs,
				blockToValidate,
			}

			err = validateState(blockDataMap[blockToValidate.Hash], true)
			if err != nil {
				return nil, errors.New(fmt.Sprintf("Block (%x) could not be statevalidated: %v\n", blockToValidate.Hash[0:8], err))
			}

			postValidate(blockDataMap[blockToValidate.Hash], true)
		} else {
			blockDataMap[blockToValidate.Hash] = blockData{
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				blockToValidate,
			}

			postValidate(blockDataMap[blockToValidate.Hash], true)
		}

		logger.Printf("Block validated: %d --> %x", blockToValidate.Height, blockToValidate.Hash[0:8])
	}

	for _, blockToValidate := range allClosedBlocks {
		if blockToValidate.Height > lastBlock.Height {
			lastBlock = blockToValidate
		}
	}

	logger.Printf("\n\n%v block(s) validated. Chain good to go.\n------------------------------------------------------------------------\n\n", len(allClosedBlocks))
	logger.Printf("Last Block: \n%v\n------------------------------------------------------------------------\n\n", lastBlock)
	logger.Printf("Current STATE: \n%v\n------------------------------------------------------------------------\n\n", getState())

	return initialBlock, nil
}

func accStateChange(txSlice []*protocol.AccTx) error {
	for _, tx := range txSlice {
		if tx.Header != 2 {
			newAcc := protocol.NewAccount(
				tx.PubKey,
				tx.Issuer,
				0,
				false,
				[crypto.COMM_KEY_LENGTH]byte{},
				tx.Contract,
				tx.ContractVariables,
				tx.ChamHashParams,
			)
			newAccHash := newAcc.Hash()

			acc, _ := storage.GetAccount(newAccHash)
			if acc != nil {
				//Shouldn't happen, because this should have been prevented when adding an accTx to the block
				return errors.New("Address already exists in the state.")
			}

			//If acc does not exist, write to state
			storage.State[newAccHash] = &newAcc

			if tx.Header == 1 {
				//First bit set, given account will be a new root account
				//It might be cleaner to move this to the storage package (e.g., storage.Delete(...))
				//leave it here for now (not fully convinced yet)
				storage.RootKeys[newAccHash] = &newAcc
			}
		} else if tx.Header == 2 {
			accHash := protocol.SerializeHashContent(tx.PubKey)
			_, err := storage.GetAccount(accHash)
			if err != nil {
				return err
			}

			//Second bit set, delete account from root account
			delete(storage.RootKeys, accHash)
		}
	}

	return nil
}

//this method does inititate the state change for aggregated Transactions.
func aggTxStateChange(txSlice []*protocol.FundsTx, initialSetup bool) (err error) {
	sort.Sort(ByTxCount(txSlice))

	if err := fundsStateChange(txSlice, initialSetup); err != nil {
		return err
	} else {
		return nil
	}
}

type ByTxCount []*protocol.FundsTx

func (a ByTxCount) Len() int           { return len(a) }
func (a ByTxCount) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByTxCount) Less(i, j int) bool { return a[i].TxCnt <= a[j].TxCnt }

func fundsStateChange(txSlice []*protocol.FundsTx, initialSetup bool) (err error) {
	for _, tx := range txSlice {

		//If transaction is in closed tx, the state was adjusted already.
		if storage.ReadClosedTx(tx.Hash()) != nil && !initialSetup {
			continue
		}

		var rootAcc *protocol.Account
		//Check if we have to issue new coins (in case a root account signed the tx)
		if rootAcc, err = storage.GetRootAccount(tx.From); err != nil {
			return err
		}

		if rootAcc != nil && rootAcc.Balance+tx.Amount+tx.Fee > MAX_MONEY {
			return errors.New("Transaction amount would lead to balance overflow at the receiver (root) account.")
		}

		//Will not be reached if errors occured
		if rootAcc != nil {
			rootAcc.Balance += tx.Amount
			rootAcc.Balance += tx.Fee
		}

		var accSender, accReceiver *protocol.Account
		accSender, err = storage.GetAccount(tx.From)
		accReceiver, err = storage.GetAccount(tx.To)

		//Check transaction counter
		if !initialSetup && tx.Aggregated == false && tx.TxCnt != accSender.TxCnt {
			if tx.TxCnt < accSender.TxCnt {
				logger.Printf("Tx %x, already in the state.", tx.Hash())
			} else {
				err = errors.New(fmt.Sprintf("Sender (%x) txCnt in %x does not match: %v (tx.txCnt) vs. %v (state txCnt).", accSender.Address[0:8], tx.Hash(), tx.TxCnt, accSender.TxCnt))
			}
		}

		//Check sender balance
		// "!initialSetup" does allow a "Credit" like behaviour where there is no error, regarding the balance. In the end it should match the wanted state.
		if !initialSetup && (tx.Amount+tx.Fee) > accSender.Balance {
			err = errors.New(fmt.Sprintf("Sender does not have enough funds for the Funds transaction: Balance = %v, Amount = %v, Fee = %v.", accSender.Balance, tx.Amount, tx.Fee))
		}

		//After Tx fees, account must still have more than the minimum staking amount
		if accSender.IsStaking && ((tx.Fee + protocol.MIN_STAKING_MINIMUM + tx.Amount) > accSender.Balance) {
			err = errors.New("Sender is staking and does not have enough funds in order to fulfill the required staking minimum.")
		}

		//Overflow protection
		if !initialSetup && tx.Amount+accReceiver.Balance > MAX_MONEY {
			err = errors.New("Transaction amount would lead to balance overflow at the receiver account.")
		}

		if err != nil {
			if rootAcc != nil {
				//Rollback root's credits if error occurs
				rootAcc.Balance -= tx.Amount
				rootAcc.Balance -= tx.Fee
			}

			return err
		}

		//We're manipulating pointer, no need to write back
		accSender.TxCnt += 1
		accSender.Balance -= tx.Amount
		accReceiver.Balance += tx.Amount
	}
	return nil
}

//We accept config slices with unknown id, but don't act on the payload. This is in case we have not updated to a new
//software with corresponding code to act on the configTx id/payload
func configStateChange(configTxSlice []*protocol.ConfigTx, blockHash [32]byte) {
	var newParameters Parameters
	//Initialize it to state right now (before validating config txs)
	newParameters = *activeParameters

	if len(configTxSlice) == 0 {
		return
	}

	//Only add a new parameter struct if a relevant system parameter changed
	if CheckAndChangeParameters(&newParameters, &configTxSlice) {
		newParameters.BlockHash = blockHash
		parameterSlice = append(parameterSlice, newParameters)
		activeParameters = &parameterSlice[len(parameterSlice)-1]
		logger.Printf("Config parameters changed. New configuration: %v", *activeParameters)
	}
}

func stakeStateChange(txSlice []*protocol.StakeTx, height uint32, initialSetup bool) (err error) {
	for _, tx := range txSlice {
		var accSender *protocol.Account
		accSender, err = storage.GetAccount(tx.Account)

		//Check staking state
		if tx.IsStaking == accSender.IsStaking {
			err = errors.New("IsStaking state is already set to " + strconv.FormatBool(accSender.IsStaking) + ".")
		}

		//Check minimum amount
		if !initialSetup && tx.IsStaking && accSender.Balance < tx.Fee+activeParameters.StakingMinimum {
			err = errors.New(fmt.Sprintf("Sender wants to stake but does not have enough funds (%v) in order to fulfill the required staking minimum (%v).", accSender.Balance, STAKING_MINIMUM))
		}

		//Check sender balance
		if !initialSetup && tx.Fee > accSender.Balance {
			err = errors.New(fmt.Sprintf("Sender (%x) does not have enough funds for the Stake transaction: Balance = %v, Amount = %v, Fee = %v.", accSender.Address[0:8], accSender.Balance, 0, tx.Fee))
		}

		if err != nil {
			return err
		}

		//We're manipulating pointer, no need to write back
		accSender.IsStaking = tx.IsStaking
		accSender.CommitmentKey = tx.CommitmentKey
		accSender.StakingBlockHeight = height
	}

	return nil
}

func getFundsTxFromAggTx(AggregatedTxSlice [][32]byte) (fundsTxSlice []*protocol.FundsTx) {

	for _, txHash := range AggregatedTxSlice {
		trx := storage.ReadOpenTx(txHash)

		//Only new funds transactions give a fee... When a transaction is in the closed state, the fee is already collected
		if trx != nil {
			switch trx.(type) {
			case *protocol.FundsTx:
				fundsTxSlice = append(fundsTxSlice, trx.(*protocol.FundsTx))
			default:
				continue
			}
		}
	}
	return fundsTxSlice
}

func collectTxFees(accTxSlice []*protocol.AccTx, fundsTxSlice []*protocol.FundsTx, configTxSlice []*protocol.ConfigTx, stakeTxSlice []*protocol.StakeTx, aggTxSlice []*protocol.AggTx, minerHash [32]byte, initialSetup bool) (err error) {
	var tmpAccTx []*protocol.AccTx
	var tmpFundsTx []*protocol.FundsTx
	var tmpConfigTx []*protocol.ConfigTx
	var tmpStakeTx []*protocol.StakeTx

	//if initialSetup { //TODO DELETE THIS
	minerAcc, err := storage.GetAccount(minerHash)
	if err != nil {
		return err
	}

	//Get all new Funds Transactions and append them to the fundsTxSlice
	for _, tx := range aggTxSlice {
		fundsTxSlice = append(fundsTxSlice, getFundsTxFromAggTx(tx.AggregatedTxSlice)...)
	}

	var senderAcc *protocol.Account

	for _, tx := range accTxSlice {
		if minerAcc.Balance+tx.Fee > MAX_MONEY {
			err = errors.New("Fee amount would lead to balance overflow at the miner account.")
		}

		if err != nil {
			//Rollback of all perviously transferred transaction fees to the protocol's account
			collectTxFeesRollback(tmpAccTx, tmpFundsTx, tmpConfigTx, tmpStakeTx, minerHash)
			return err
		}

		//Money gets created from thin air, no need to subtract money from root key
		minerAcc.Balance += tx.Fee
		tmpAccTx = append(tmpAccTx, tx)
	}

	//subtract fees from sender (check if that is allowed has already been done in the block validation)
	for _, tx := range fundsTxSlice {
		//Prevent protocol account from overflowing
		if minerAcc.Balance+tx.Fee > MAX_MONEY {
			err = errors.New("Fee amount would lead to balance overflow at the miner account.")
		}

		senderAcc, err = storage.GetAccount(tx.From)

		if err != nil {
			//Rollback of all perviously transferred transaction fees to the protocol's account
			collectTxFeesRollback(tmpAccTx, tmpFundsTx, tmpConfigTx, tmpStakeTx, minerHash)
			return err
		}

		minerAcc.Balance += tx.Fee
		senderAcc.Balance -= tx.Fee
		tmpFundsTx = append(tmpFundsTx, tx)
	}

	for _, tx := range configTxSlice {
		if minerAcc.Balance+tx.Fee > MAX_MONEY {
			err = errors.New("Fee amount would lead to balance overflow at the miner account.")
		}

		if err != nil {
			//Rollback of all perviously transferred transaction fees to the protocol's account
			collectTxFeesRollback(tmpAccTx, tmpFundsTx, tmpConfigTx, tmpStakeTx, minerHash)
			return err
		}

		//No need to subtract money because signed by root account
		minerAcc.Balance += tx.Fee
		tmpConfigTx = append(tmpConfigTx, tx)
	}

	for _, tx := range stakeTxSlice {
		if minerAcc.Balance+tx.Fee > MAX_MONEY {
			err = errors.New("Fee amount would lead to balance overflow at the miner account.")
		}

		senderAcc, err = storage.GetAccount(tx.Account)

		if err != nil {
			//Rollback of all perviously transferred transaction fees to the protocol's account
			collectTxFeesRollback(tmpAccTx, tmpFundsTx, tmpConfigTx, tmpStakeTx, minerHash)
			return err
		}

		senderAcc.Balance -= tx.Fee
		minerAcc.Balance += tx.Fee
		tmpStakeTx = append(tmpStakeTx, tx)
	}
	return nil
}

func collectBlockReward(reward uint64, minerHash [32]byte, initialSetup bool) (err error) {
	//if initialSetup {
	var miner *protocol.Account
	miner, err = storage.GetAccount(minerHash)

	if !initialSetup && miner.Balance+reward > MAX_MONEY {
		err = errors.New("Block reward would lead to balance overflow at the miner account.")
	}

	if err != nil {
		return err
	}

	miner.Balance += reward
	return nil
}

func collectSlashReward(reward uint64, block *protocol.Block) (err error) {
	//Check if proof is provided. If proof was incorrect, prevalidation would already have failed.
	if block.SlashedAddress != [32]byte{} || block.ConflictingBlockHash1 != [32]byte{} || block.ConflictingBlockHash2 != [32]byte{} || block.ConflictingBlockHashWithoutTx1 != [32]byte{} || block.ConflictingBlockHashWithoutTx2 != [32]byte{} {
		var minerAcc, slashedAcc *protocol.Account
		minerAcc, err = storage.GetAccount(block.Beneficiary)
		slashedAcc, err = storage.GetAccount(block.SlashedAddress)

		if minerAcc.Balance+reward > MAX_MONEY {
			err = errors.New("Slash reward would lead to balance overflow at the miner account.")
		}

		if err != nil {
			return err
		}

		//Validator is rewarded with slashing reward for providing a valid slashing proof
		minerAcc.Balance += reward
		//Slashed account looses the minimum staking amount
		slashedAcc.Balance -= activeParameters.StakingMinimum
		//Slashed account is being removed from the validator set
		slashedAcc.IsStaking = false
	}

	return nil
}

//No rollback method exists
func updateStakingHeight(block *protocol.Block) error {
	acc, err := storage.GetAccount(block.Beneficiary)
	if err != nil {
		return err
	}

	acc.StakingBlockHeight = block.Height

	return nil
}
