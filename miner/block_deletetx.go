package miner

import (
	"errors"
	"fmt"
	"github.com/julwil/bazo-miner/p2p"
	"github.com/julwil/bazo-miner/protocol"
	"github.com/julwil/bazo-miner/storage"
	"time"
)

// Handles the processing of a DeleteTx.
// Deletes the tx to delete referenced by tx.TxToDeleteHash from the
// local storage and removes the hash from the tx from the block it was included in.
// Adds the DeleteTx to the DeleteTxData slice of the current block.
func addDeleteTx(b *protocol.Block, tx *protocol.DeleteTx) error {

	// First we perform the deletion of the tx we want to remove.
	handleTxDeletion(tx)

	// Then we can include the DeleteTx in the current block.
	b.DeleteTxData = append(b.DeleteTxData, tx.Hash())

	return nil
}

// Handles the processing of a given DeleteTx
func handleTxDeletion(deleteTx *protocol.DeleteTx) error {

	txToDeleteHash := deleteTx.TxToDeleteHash
	deleteTxHash := deleteTx.Hash()

	// At this point we already verified that the transaction we want to delete actually exists
	// either in the open or closed transaction storage. Thus we can safely assume it exists and
	// delete it from our local storage.
	deleteTxFromLocalStorage(txToDeleteHash)

	// We also need to remove the hash of the tx from the block it was included in, when tx was initially mined.
	// To this end we locate the block first and then replace the txToDeleteHash with the deleteTxHash.
	blockToUpdate := storage.ReadBlockByTxHash(txToDeleteHash) // TODO implement a linear scan on blocks to locate the blockToUpdate. The ReadBlockByTxHash method is only here for convenience and will be removed in the future.
	if blockToUpdate == nil {
		return errors.New(fmt.Sprintf("Can't find block of tx: %x", txToDeleteHash))
	}

	// Before we do any changes on the block, we need to store the original hash input before the mutation.
	oldSha3Hash := blockToUpdate.Sha3Hash()
	oldHashInput := oldSha3Hash[:]

	// We also need to replace the hash of the tx to delete with the hash of the delete-tx
	// from the block it was included in when tx was initially mined.
	// First we get the block form the local storage where the tx was included in.
	err := findReplaceTxInBlock(txToDeleteHash, deleteTxHash, blockToUpdate)
	if err != nil {
		return errors.New(fmt.Sprintf("\nRemoving \ntx: %x from \nblock: %x failed.", txToDeleteHash, blockToUpdate.Hash))
	}

	// We need to rebuild the block and make sure hash consistency is maintained.
	finalizeBlockAfterMutation(blockToUpdate, &oldHashInput)

	// Update the block in the local storage.
	storage.DeleteOpenBlock(blockToUpdate.Hash)
	storage.WriteClosedBlock(blockToUpdate)

	go broadcastBlock(blockToUpdate)
	logger.Printf("\nBroadcasted updated Block:\n%s", blockToUpdate.String())

	return nil
}

// Deletes a given tx from the local storage
func deleteTxFromLocalStorage(txHash [32]byte) error {
	var txToDelete protocol.Transaction

	switch true {
	case storage.ReadOpenTx(txHash) != nil:
		txToDelete = storage.ReadOpenTx(txHash)
		storage.DeleteOpenTx(txToDelete)

		if storage.ReadOpenTx(txHash) == nil {
			logger.Printf("\nTx: %x was deleted from open transaction storage.", txHash)
		}

	case storage.ReadClosedTx(txHash) != nil:
		txToDelete = storage.ReadClosedTx(txHash)
		storage.DeleteClosedTx(txToDelete)

		if storage.ReadClosedTx(txHash) == nil {
			logger.Printf("\nTx: %x was deleted from closed transaction storage.", txHash)
		}

	default: // If we don't find the tx to delete in the storage, we also can't delete it.

		return errors.New(fmt.Sprintf("Can't find TxToDelete: %x", txHash))
	}

	return nil
}

// Finds and replaces a given txToFind with a txToReplace in a given block
func findReplaceTxInBlock(toFind [32]byte, toReplace [32]byte, block *protocol.Block) error {

	for {

		// Agg
		for i, aggTxHash := range block.AggTxData {
			if aggTxHash == toFind {
				block.AggTxData[i] = toReplace
				block.NrUpdates++
				logger.Printf("\nLocated tx in AggTxData block slice")

				return nil
			}
		}

		// Funds
		for i, fundsTxHash := range block.FundsTxData {
			if fundsTxHash == toFind {
				block.FundsTxData = remove(block.FundsTxData, i) // TODO: removal is only a temp. solution. We need to swap the deleteTx and txToDelete in the slice.
				block.NrFundsTx--
				block.NrUpdates++

				return nil
			}
		}

		// Accounts
		for i, accTxHash := range block.AccTxData {
			if accTxHash == toFind {
				block.AccTxData[i] = toReplace
				block.NrUpdates++

				return nil
			}
		}

		// Config
		for i, configTxHash := range block.ConfigTxData {
			if configTxHash == toFind {
				block.ConfigTxData[i] = toReplace
				block.NrUpdates++

				return nil
			}
		}

		// Delete
		for i, deleteTxHash := range block.DeleteTxData {
			if deleteTxHash == toFind {
				block.DeleteTxData[i] = toReplace
				block.NrUpdates++

				return nil
			}
		}

		return errors.New(fmt.Sprintf("Can't find TxToDelete: %x", toFind))
	}
}

// Fetch DeleteTxData
func fetchDeleteTxData(block *protocol.Block, deleteTxSlice []*protocol.DeleteTx, initialSetup bool, errChan chan error) {
	for i, txHash := range block.DeleteTxData {
		var tx protocol.Transaction
		var deleteTx *protocol.DeleteTx

		closedTx := storage.ReadClosedTx(txHash)
		if closedTx != nil {
			logger.Printf("Tx was in closed")
			if initialSetup {
				deleteTx = closedTx.(*protocol.DeleteTx)
				deleteTxSlice[i] = deleteTx
				continue
			} else {
				//Reject blocks that have txs which have already been validated.
				errChan <- errors.New("Block validation had deleteTx that was already in a previous block.")
				return
			}
		}

		//Tx is either in open storage or needs to be fetched from the network.
		tx = storage.ReadOpenTx(txHash)
		if tx != nil {
			logger.Printf("Tx was in open")
			deleteTx = tx.(*protocol.DeleteTx)
		} else {
			err := p2p.TxReq(txHash, p2p.DELTX_REQ)
			if err != nil {
				errChan <- errors.New(fmt.Sprintf("DeleteTx could not be read: %v", err))
				return
			}

			//Blocking Wait
			select {
			case deleteTx = <-p2p.DeleteTxChan:
				logger.Printf("Tx was fetched from network")
			case <-time.After(TXFETCH_TIMEOUT * time.Second):
				errChan <- errors.New("DeleteTx fetch timed out.")
			}
			//This check is important. A malicious miner might have sent us a tx whose hash is a different one
			//from what we requested.
			if deleteTx.Hash() != txHash {
				errChan <- errors.New("Received DeleteTxHash did not correspond to our request.")
			}
		}

		deleteTxSlice[i] = deleteTx
	}

	errChan <- nil
}

// Removes item at index i from the slice s.
// Returns the updated slice.
func remove(slice [][32]byte, i int) [][32]byte {
	slice[len(slice)-1], slice[i] = slice[i], slice[len(slice)-1]

	return slice[:len(slice)-1]
}
