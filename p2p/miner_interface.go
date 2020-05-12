package p2p

import (
	"github.com/julwil/bazo-miner/protocol"
	"github.com/julwil/bazo-miner/storage"
	"sync"
)

var (
	//Block from the network, to the miner
	BlockIn = make(chan []byte, 1000)
	//Block from the miner, to the network
	BlockOut = make(chan []byte, 100)
	//BlockHeader from the miner, to the clients
	BlockHeaderOut = make(chan []byte)

	VerifiedTxsOut       = make(chan []byte)
	VerifiedTxsBrdcstOut = make(chan []byte, 1000)

	//Data requested by miner, to allow parallelism, we have a chan for every tx type.
	FundsTxChan  = make(chan *protocol.FundsTx)
	AccTxChan    = make(chan *protocol.AccTx)
	ConfigTxChan = make(chan *protocol.ConfigTx)
	StakeTxChan  = make(chan *protocol.StakeTx)
	AggTxChan    = make(chan *protocol.AggTx)
	DeleteTxChan = make(chan *protocol.DeleteTx)

	BlockReqChan = make(chan []byte)

	ReceivedFundsTXStash  = make([]*protocol.FundsTx, 0)
	ReceivedAggTxStash    = make([]*protocol.AggTx, 0)
	ReceivedStakeTxStash  = make([]*protocol.StakeTx, 0)
	ReceivedAccTxStash    = make([]*protocol.AccTx, 0)
	ReceivedDeleteTxStash = make([]*protocol.DeleteTx, 0)

	fundsTxSashMutex   = &sync.Mutex{}
	aggTxStashMutex    = &sync.Mutex{}
	blockStashMutex    = &sync.Mutex{}
	stakeTxStashMutex  = &sync.Mutex{}
	accTxStashMutex    = &sync.Mutex{}
	deleteTxStashMutex = &sync.Mutex{}
)

//This is for blocks and txs that the miner successfully validated.
func forwardBlockBrdcstToMiner() {
	for {
		block := <-BlockOut
		toBrdcst := BuildPacket(BLOCK_BRDCST, block)

		minerBrdcstMsg <- toBrdcst
	}
}

func forwardBlockHeaderBrdcstToMiner() {
	for {
		blockHeader := <-BlockHeaderOut
		clientBrdcstMsg <- BuildPacket(BLOCK_HEADER_BRDCST, blockHeader)
	}
}

func forwardVerifiedTxsToMiner() {
	for {
		verifiedTxs := <-VerifiedTxsOut
		clientBrdcstMsg <- BuildPacket(VERIFIEDTX_BRDCST, verifiedTxs)
	}
}

func forwardVerifiedTxsBrdcstToMiner() {
	for {
		verifiedTx := <-VerifiedTxsBrdcstOut
		minerBrdcstMsg <- verifiedTx
	}
}

func processBlockBrdcst(p *peer, payload []byte) {
	if len(BlockIn) > 0 {
		var block *protocol.Block
		block = block.Decode(payload)
		logger.Printf("Inside ForwardBlockToMiner --> len(BlockIn) = %v for block %x", len(BlockIn), block.Hash[0:8])
	}

	BlockIn <- payload
}

//Checks if Tx Is in the received stash. If true, we received the transaction with a request already.
func FundsTxAlreadyInStash(slice []*protocol.FundsTx, newTXHash [32]byte) bool {
	for _, txInStash := range slice {
		if txInStash.Hash() == newTXHash {
			return true
		}
	}
	return false
}

func AggTxAlreadyInStash(slice []*protocol.AggTx, newTXHash [32]byte) bool {
	for _, txInStash := range slice {
		if txInStash.Hash() == newTXHash {
			return true
		}
	}
	return false
}

func StakeTxAlreadyInStash(slice []*protocol.StakeTx, newTXHash [32]byte) bool {
	for _, txInStash := range slice {
		if txInStash.Hash() == newTXHash {
			return true
		}
	}
	return false
}

func AccTxAlreadyInStash(slice []*protocol.AccTx, newTXHash [32]byte) bool {
	for _, txInStash := range slice {
		if txInStash.Hash() == newTXHash {
			return true
		}
	}
	return false
}

func DeleteTxAlreadyInStash(slice []*protocol.DeleteTx, newTXHash [32]byte) bool {
	for _, txInStash := range slice {
		if txInStash.Hash() == newTXHash {
			return true
		}
	}
	return false
}

func BlockAlreadyReceived(slice []*protocol.Block, newBlockHash [32]byte) bool {
	for _, block := range slice {
		if block.Hash == newBlockHash {
			return true
		}
	}
	return false
}

//These are transactions the miner specifically requested.
func forwardTxReqToMiner(p *peer, payload []byte, txType uint8) {
	if payload == nil {
		return
	}

	switch txType {
	case FUNDSTX_RES:
		var fundsTx *protocol.FundsTx
		fundsTx = fundsTx.Decode(payload)
		if fundsTx == nil {
			return
		}
		// If TX is not received with the last 1000 Transaction, send it through the channel to the TX_FETCH.
		// Otherwise send nothing. This means, that the TX was sent before and we ensure, that only one TX per Broadcast
		// request is going through to the FETCH Request. This should prevent the "Received txHash did not correspond to
		// our request." error
		// The Mutex Lock is needed, because sometimes the execution is too fast. And even with the stash transactions
		// are sent multiple times through the channel.
		// The same concept is used for the AggTx below.
		fundsTxSashMutex.Lock()
		if !FundsTxAlreadyInStash(ReceivedFundsTXStash, fundsTx.Hash()) {
			ReceivedFundsTXStash = append(ReceivedFundsTXStash, fundsTx)
			FundsTxChan <- fundsTx
			if len(ReceivedFundsTXStash) > 100 {
				ReceivedFundsTXStash = append(ReceivedFundsTXStash[:0], ReceivedFundsTXStash[1:]...)
			}
		}
		fundsTxSashMutex.Unlock()
	case ACCTX_RES:
		var accTx *protocol.AccTx
		accTx = accTx.Decode(payload)
		if accTx == nil {
			return
		}
		accTxStashMutex.Lock()
		if !AccTxAlreadyInStash(ReceivedAccTxStash, accTx.Hash()) {
			ReceivedAccTxStash = append(ReceivedAccTxStash, accTx)
			AccTxChan <- accTx
			if len(ReceivedAccTxStash) > 100 {
				ReceivedAccTxStash = append(ReceivedAccTxStash[:0], ReceivedAccTxStash[1:]...)
			}
		}
		accTxStashMutex.Unlock()
	case CONFIGTX_RES:
		var configTx *protocol.ConfigTx
		configTx = configTx.Decode(payload)
		if configTx == nil {
			return
		}
		ConfigTxChan <- configTx
	case STAKETX_RES:
		var stakeTx *protocol.StakeTx
		stakeTx = stakeTx.Decode(payload)
		if stakeTx == nil {
			return
		}

		stakeTxStashMutex.Lock()
		if !StakeTxAlreadyInStash(ReceivedStakeTxStash, stakeTx.Hash()) {
			ReceivedStakeTxStash = append(ReceivedStakeTxStash, stakeTx)
			StakeTxChan <- stakeTx
			if len(ReceivedStakeTxStash) > 100 {
				ReceivedStakeTxStash = append(ReceivedStakeTxStash[:0], ReceivedStakeTxStash[1:]...)
			}
		}
		stakeTxStashMutex.Unlock()
	case AGGTX_RES:
		var aggTx *protocol.AggTx
		aggTx = aggTx.Decode(payload)
		if aggTx == nil {
			return
		}

		aggTxStashMutex.Lock()
		if !AggTxAlreadyInStash(ReceivedAggTxStash, aggTx.Hash()) {
			ReceivedAggTxStash = append(ReceivedAggTxStash, aggTx)
			AggTxChan <- aggTx
			if len(ReceivedAggTxStash) > 100 {
				ReceivedAggTxStash = append(ReceivedAggTxStash[:0], ReceivedAggTxStash[1:]...)
			}
		}
		aggTxStashMutex.Unlock()

	case DELTX_RES:
		var deleteTx *protocol.DeleteTx
		deleteTx = deleteTx.Decode(payload)
		if deleteTx == nil {
			return
		}
		deleteTxStashMutex.Lock()
		if !DeleteTxAlreadyInStash(ReceivedDeleteTxStash, deleteTx.Hash()) {
			ReceivedDeleteTxStash = append(ReceivedDeleteTxStash, deleteTx)
			DeleteTxChan <- deleteTx
			if len(ReceivedDeleteTxStash) > 100 {
				ReceivedDeleteTxStash = append(ReceivedDeleteTxStash[:0], ReceivedDeleteTxStash[1:]...)
			}
		}
		deleteTxStashMutex.Unlock()
	}
}

func forwardBlockReqToMiner(p *peer, payload []byte) {
	var block *protocol.Block
	block = block.Decode(payload)

	blockStashMutex.Lock()
	if !BlockAlreadyReceived(storage.ReadReceivedBlockStash(), block.Hash) {
		storage.WriteToReceivedStash(block)
		BlockReqChan <- payload
	}
	blockStashMutex.Unlock()
}

func ReadSystemTime() int64 {
	return systemTime
}
