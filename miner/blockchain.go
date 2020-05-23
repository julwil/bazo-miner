package miner

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"github.com/julwil/bazo-miner/crypto"
	"github.com/julwil/bazo-miner/p2p"
	"github.com/julwil/bazo-miner/protocol"
	"github.com/julwil/bazo-miner/storage"
	"log"
	"sync"
)

var (
	logger                       *log.Logger
	blockValidation              = &sync.Mutex{}
	parameterSlice               []Parameters
	activeParameters             *Parameters
	uptodate                     bool
	slashingDict                 = make(map[[32]byte]SlashingProof)
	validatorAccAddress          [64]byte
	multisigPubKey               *ecdsa.PublicKey
	commPrivKey, rootCommPrivKey *rsa.PrivateKey
)

//Miner entry point
func Init(
	validatorWallet,
	multisigWallet,
	rootWallet *ecdsa.PublicKey,
	validatorCommitment,
	rootCommitment *rsa.PrivateKey,
	chamHashParams *crypto.ChameleonHashParameters,
) {
	var err error

	validatorAccAddress = crypto.GetAddressFromPubKey(validatorWallet)
	multisigPubKey = multisigWallet
	commPrivKey = validatorCommitment
	rootCommPrivKey = rootCommitment

	//Set up logger.
	logger = storage.InitLogger()
	logger.Printf("\n\n\n" +
		"BBBBBBBBBBBBBBBBB               AAA               ZZZZZZZZZZZZZZZZZZZ     OOOOOOOOO\n" +
		"B::::::::::::::::B             A:::A              Z:::::::::::::::::Z   OO:::::::::OO\n" +
		"B::::::BBBBBB:::::B           A:::::A             Z:::::::::::::::::Z OO:::::::::::::OO\n" +
		"BB:::::B     B:::::B         A:::::::A            Z:::ZZZZZZZZ:::::Z O:::::::OOO:::::::O\n" +
		"  B::::B     B:::::B        A:::::::::A           ZZZZZ     Z:::::Z  O::::::O   O::::::O\n" +
		"  B::::B     B:::::B       A:::::A:::::A                  Z:::::Z    O:::::O     O:::::O\n" +
		"  B::::BBBBBB:::::B       A:::::A A:::::A                Z:::::Z     O:::::O     O:::::O\n" +
		"  B:::::::::::::BB       A:::::A   A:::::A              Z:::::Z      O:::::O     O:::::O\n" +
		"  B::::BBBBBB:::::B     A:::::A     A:::::A            Z:::::Z       O:::::O     O:::::O\n" +
		"  B::::B     B:::::B   A:::::AAAAAAAAA:::::A          Z:::::Z        O:::::O     O:::::O\n" +
		"  B::::B     B:::::B  A:::::::::::::::::::::A        Z:::::Z         O:::::O     O:::::O\n" +
		"  B::::B     B:::::B A:::::AAAAAAAAAAAAA:::::A    ZZZ:::::Z     ZZZZZO::::::O   O::::::O\n" +
		"BB:::::BBBBBB::::::BA:::::A             A:::::A   Z::::::ZZZZZZZZ:::ZO:::::::OOO:::::::O\n" +
		"B:::::::::::::::::BA:::::A               A:::::A  Z:::::::::::::::::Z OO:::::::::::::OO\n" +
		"B::::::::::::::::BA:::::A                 A:::::A Z:::::::::::::::::Z   OO:::::::::OO\n" +
		"BBBBBBBBBBBBBBBBBAAAAAAA                   AAAAAAAZZZZZZZZZZZZZZZZZZZ     OOOOOOOOO\n\n\n")

	logger.Printf("\n\n\n-------------------- START MINER ---------------------")
	logger.Printf("This Miners IP-Address: %v\n\n", p2p.Ipport)
	parameterSlice = append(parameterSlice, NewDefaultParameters())
	activeParameters = &parameterSlice[0]

	//Initialize root key.
	initRootKey(rootWallet, chamHashParams)
	if err != nil {
		logger.Printf("Could not create a root account.\n")
	}

	currentTargetTime = new(timerange)
	target = append(target, 13)

	initialBlock, err := initState()
	if err != nil {
		logger.Printf("Could not set up initial state: %v.\n", err)
		return
	}

	logger.Printf("ActiveConfigParams: \n%v\n------------------------------------------------------------------------\n\nBAZO is Running\n\n", activeParameters)

	//this is used to generate the state with aggregated transactions.
	for _, tx := range storage.ReadAllBootstrapReceivedTransactions() {
		if tx != nil {
			storage.DeleteOpenTx(tx)
			storage.WriteClosedTx(tx)
		}
	}
	storage.DeleteBootstrapReceivedMempool()

	//Start to listen to network inputs (txs and blocks).
	go incomingData()
	mining(initialBlock)
}

//Mining is a constant process, trying to come up with a successful PoW.
func mining(initialBlock *protocol.Block) {
	currentBlock := newBlock(initialBlock.Hash, initialBlock.HashWithoutTx, [crypto.COMM_PROOF_LENGTH]byte{}, initialBlock.Height+1)

	for {
		err := finalizeBlock(currentBlock)
		if err != nil {
			logger.Printf("%v\n", err)
		} else {
			logger.Printf("Block mined (%x)\n", currentBlock.Hash[0:8])
		}

		if err == nil {
			err := validate(currentBlock, false)
			if err == nil {
				//Only broadcast the block if it is valid.
				go broadcastBlock(currentBlock)
				logger.Printf("Validated block (mined): %vState:\n%v", currentBlock, getState())
			} else {
				logger.Printf("Mined block (%x) could not be validated: %v\n", currentBlock.Hash[0:8], err)
			}
		}

		//Prints miner connections
		p2p.EmptyingiplistChan()
		p2p.PrintMinerConns()

		//This is the same mutex that is claimed at the beginning of a block validation. The reason we do this is
		//that before start mining a new block we empty the mempool which contains tx data that is likely to be
		//validated with block validation, so we wait in order to not work on tx data that is already validated
		//when we finish the block.
		logger.Printf("\n\n __________________________________________________ New Mining Round __________________________________________________")
		blockValidation.Lock()
		logger.Printf("Create Next Block")
		nextBlock := newBlock(lastBlock.Hash, lastBlock.HashWithoutTx, [crypto.COMM_PROOF_LENGTH]byte{}, lastBlock.Height+1)
		currentBlock = nextBlock
		logger.Printf("Prepare Next Block")
		prepareBlock(currentBlock)
		logger.Printf("Prepare Next Block --> Done")
		blockValidation.Unlock()
	}
}

//At least one root key needs to be set which is allowed to create new accounts.
func initRootKey(rootKey *ecdsa.PublicKey, chamHashParams *crypto.ChameleonHashParameters) error {
	address := crypto.GetAddressFromPubKey(rootKey)
	addressHash := protocol.SerializeHashContent(address)

	var commPubKey [crypto.COMM_KEY_LENGTH]byte
	copy(commPubKey[:], rootCommPrivKey.N.Bytes())

	rootAcc := protocol.NewAccount(
		address,
		[32]byte{},
		activeParameters.StakingMinimum,
		true,
		commPubKey,
		nil,
		nil,
		chamHashParams,
	)
	storage.State[addressHash] = &rootAcc
	storage.RootKeys[addressHash] = &rootAcc
	storage.ChamHashParams = chamHashParams

	return nil
}
