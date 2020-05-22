package storage

import (
	"errors"
	"fmt"
	"github.com/julwil/bazo-miner/protocol"
	"io"
	"log"
	"os"
	"time"
)

func InitLogger() *log.Logger {

	//Create a Log-file (Logger.Miner.log) and write all logger.printf(...) Statements into it.

	//use this two lines, if all miners should have distinct names for their log files.
	time.Now().Format("030405")
	filename := "LoggerMiner" + time.Now().Format("150405") + ".log"

	//use this line when all miners should have the same log file name.
	//filename := "LoggerMiner.log"
	LogFile, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}

	wrt := io.MultiWriter(os.Stdout, LogFile)
	log.SetOutput(wrt)
	return log.New(wrt, "INFO: ", log.Ldate|log.Lmicroseconds|log.Lshortfile)
}

//Needed by miner and p2p package
func GetAccount(hash [32]byte) (acc *protocol.Account, err error) {
	if acc = State[hash]; acc != nil {
		return acc, nil
	} else {
		return nil, errors.New(fmt.Sprintf("Acc (%x) not in the state.", hash[0:8]))
	}
}

func GetRootAccount(hash [32]byte) (acc *protocol.Account, err error) {
	if IsRootKey(hash) {
		acc, err = GetAccount(hash)
		return acc, err
	}

	return nil, err
}

func IsRootKey(hash [32]byte) bool {
	_, exists := RootKeys[hash]
	return exists
}

//Get all pubKeys involved in AccTx, FundsTx of a given block
func GetTxPubKeys(block *protocol.Block) (txPubKeys [][32]byte) {
	var accTxs [][32]byte
	var fundsTxs [][32]byte

	for _, txHash := range append(block.AccTxData, block.FundsTxData...) {
		var tx protocol.Transaction

		tx = ReadClosedTx(txHash)
		if tx == nil {
			tx = ReadOpenTx(txHash)
		}

		// Upon deletion of a tx, we replace the original hash with the DeleteTx hash. We have to filter the by type.
		switch tx.(type) {
		case *protocol.AccTx:
			accTxs = append(accTxs, txHash)
		case *protocol.FundsTx:
			fundsTxs = append(fundsTxs, txHash)
		default: // In case we come across a DeleteTx or an unknown type.
			continue
		}
	}

	return append(GetAccTxPubKeys(accTxs), GetFundsTxPubKeys(fundsTxs)...)
}

//Get all pubKey involved in AccTx
func GetAccTxPubKeys(accTxData [][32]byte) (accTxPubKeys [][32]byte) {
	for _, txHash := range accTxData {
		var tx protocol.Transaction
		var accTx *protocol.AccTx

		tx = ReadClosedTx(txHash)
		if tx == nil {
			tx = ReadOpenTx(txHash)
		}

		accTx = tx.(*protocol.AccTx)
		accTxPubKeys = append(accTxPubKeys, accTx.Issuer)
		accTxPubKeys = append(accTxPubKeys, protocol.SerializeHashContent(accTx.PubKey))
	}

	return accTxPubKeys
}

//Get all pubKey involved in FundsTx
func GetFundsTxPubKeys(fundsTxData [][32]byte) (fundsTxPubKeys [][32]byte) {
	for _, txHash := range fundsTxData {
		var tx protocol.Transaction
		var fundsTx *protocol.FundsTx

		tx = ReadClosedTx(txHash)
		if tx == nil {
			tx = ReadOpenTx(txHash)
		}

		fundsTx = tx.(*protocol.FundsTx)
		fundsTxPubKeys = append(fundsTxPubKeys, fundsTx.From)
		fundsTxPubKeys = append(fundsTxPubKeys, fundsTx.To)
	}

	return fundsTxPubKeys
}
