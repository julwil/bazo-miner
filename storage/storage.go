package storage

import (
	"fmt"
	"github.com/julwil/bazo-miner/crypto"
	"log"
	"sync"
	"time"

	"github.com/boltdb/bolt"
	"github.com/julwil/bazo-miner/protocol"
)

var (
	db                                *bolt.DB
	logger                            *log.Logger
	State                             = make(map[[32]byte]*protocol.Account)
	RootKeys                          = make(map[[32]byte]*protocol.Account)
	ChamHashParams                    *crypto.ChameleonHashParameters
	txMemPool                         = make(map[[32]byte]protocol.Transaction)
	txINVALIDMemPool                  = make(map[[32]byte]protocol.Transaction)
	bootstrapReceivedMemPool          = make(map[[32]byte]protocol.Transaction)
	DifferentSenders                  = make(map[[32]byte]uint32)
	DifferentReceivers                = make(map[[32]byte]uint32)
	FundsTxBeforeAggregation          = make([]*protocol.FundsTx, 0)
	ReceivedBlockStash                = make([]*protocol.Block, 0)
	TxcntToTxMap                      = make(map[uint32][][32]byte)
	AllClosedBlocksAsc                []*protocol.Block
	Bootstrap_Server                  string
	averageTxSize                     float32 = 0
	totalTransactionSize              float32 = 0
	nrClosedTransactions              float32 = 0
	openTxMutex                               = &sync.Mutex{}
	openINVALIDTxMutex                        = &sync.Mutex{}
	openFundsTxBeforeAggregationMutex         = &sync.Mutex{}
	txcntToTxMapMutex                         = &sync.Mutex{}
	ReceivedBlockStashMutex                   = &sync.Mutex{}
)

const (
	ERROR_MSG = "Initiate storage aborted: "
)

//Entry function for the storage package
func Init(dbname string, bootstrapIpport string) {
	Bootstrap_Server = bootstrapIpport
	logger = InitLogger()

	var err error
	db, err = bolt.Open(dbname, 0600, &bolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		logger.Fatal(ERROR_MSG, err)
	}

	//Check if db file is empty for all non-bootstraping miners
	//if ipport != BOOTSTRAP_SERVER_PORT {
	//	err := db.View(func(tx *bolt.Tx) error {
	//		err := tx.ForEach(func(name []byte, bkt *bolt.Bucket) error {
	//			err := bkt.ForEach(func(k, v []byte) error {
	//				if k != nil && v != nil {
	//					return errors.New("Non-empty database given.")
	//				}
	//				return nil
	//			})
	//			return err
	//		})
	//		return err
	//	})
	//
	//	if err != nil {
	//		logger.Fatal(ERROR_MSG, err)
	//	}
	//}

	db.Update(func(tx *bolt.Tx) error {
		_, err = tx.CreateBucket([]byte("openblocks"))
		if err != nil {
			return fmt.Errorf(ERROR_MSG+"Create bucket: %s", err)
		}
		return nil
	})
	db.Update(func(tx *bolt.Tx) error {
		_, err = tx.CreateBucket([]byte("closedblocks"))
		if err != nil {
			return fmt.Errorf(ERROR_MSG+"Create bucket: %s", err)
		}
		return nil
	})
	db.Update(func(tx *bolt.Tx) error {
		_, err = tx.CreateBucket([]byte("closedblockswithouttx"))
		if err != nil {
			return fmt.Errorf(ERROR_MSG+"Create bucket: %s", err)
		}
		return nil
	})
	db.Update(func(tx *bolt.Tx) error {
		_, err = tx.CreateBucket([]byte("closedfunds"))
		if err != nil {
			return fmt.Errorf(ERROR_MSG+"Create bucket: %s", err)
		}
		return nil
	})
	db.Update(func(tx *bolt.Tx) error {
		_, err = tx.CreateBucket([]byte("closedaccs"))
		if err != nil {
			return fmt.Errorf(ERROR_MSG+"Create bucket: %s", err)
		}
		return nil
	})
	db.Update(func(tx *bolt.Tx) error {
		_, err = tx.CreateBucket([]byte("closedstakes"))
		if err != nil {
			return fmt.Errorf(ERROR_MSG+"Create bucket: %s", err)
		}
		return nil
	})
	db.Update(func(tx *bolt.Tx) error {
		_, err = tx.CreateBucket([]byte("closedaggregations"))
		if err != nil {
			return fmt.Errorf(ERROR_MSG+"Create bucket: %s", err)
		}
		return nil
	})
	db.Update(func(tx *bolt.Tx) error {
		_, err = tx.CreateBucket([]byte("closedconfigs"))
		if err != nil {
			return fmt.Errorf(ERROR_MSG+"Create bucket: %s", err)
		}
		return nil
	})
	db.Update(func(tx *bolt.Tx) error {
		_, err = tx.CreateBucket([]byte("closeddeletes"))
		if err != nil {
			return fmt.Errorf(ERROR_MSG+"Create bucket: %s", err)
		}
		return nil
	})
	db.Update(func(tx *bolt.Tx) error {
		_, err = tx.CreateBucket([]byte("lastclosedblock"))
		if err != nil {
			return fmt.Errorf(ERROR_MSG+"Create bucket: %s", err)
		}
		return nil
	})
	db.Update(func(tx *bolt.Tx) error {
		_, err = tx.CreateBucket([]byte("blocksbytxhash"))
		if err != nil {
			return fmt.Errorf(ERROR_MSG+"Create bucket: %s", err)
		}
		return nil
	})
}

func TearDown() {
	db.Close()
}
