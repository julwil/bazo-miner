package p2p

//All incoming messages are processed here and acted upon accordingly
func processIncomingMsg(p *peer, header *Header, payload []byte) {

	switch header.TypeID {
	//BROADCASTING
	case FUNDSTX_BRDCST:
		processTxBrdcst(p, payload, FUNDSTX_BRDCST)
	case ACCTX_BRDCST:
		processTxBrdcst(p, payload, ACCTX_BRDCST)
	case DELTX_BRDCST:
		processTxBrdcst(p, payload, DELTX_BRDCST)
	case CONFIGTX_BRDCST:
		processTxBrdcst(p, payload, CONFIGTX_BRDCST)
	case STAKETX_BRDCST:
		processTxBrdcst(p, payload, STAKETX_BRDCST)
	case AGGTX_BRDCST:
		processTxBrdcst(p, payload, AGGTX_BRDCST)
	case BLOCK_BRDCST:
		processBlockBrdcst(p, payload)
	case TIME_BRDCST:
		processTimeRes(p, payload)

		//REQUESTS
	case FUNDSTX_REQ:
		txRes(p, payload, FUNDSTX_REQ)
	case ACCTX_REQ:
		txRes(p, payload, ACCTX_REQ)
	case CONFIGTX_REQ:
		txRes(p, payload, CONFIGTX_REQ)
	case STAKETX_REQ:
		txRes(p, payload, STAKETX_REQ)
	case AGGTX_REQ:
		txRes(p, payload, AGGTX_REQ)
	case DELTX_REQ:
		txRes(p, payload, DELTX_REQ)
	case UNKNOWNTX_REQ:
		txRes(p, payload, UNKNOWNTX_REQ)
	case SPECIALTX_REQ:
		specialTxRes(p, payload, SPECIALTX_REQ)
	case NOT_FOUND_TX_REQ:
		if !peerSelfConn(p.getIPPort()) {
			notFoundTxRes(payload)
		}
	case BLOCK_REQ:
		blockRes(p, payload)
	case BLOCK_HEADER_REQ:
		blockHeaderRes(p, payload)
	case ACC_REQ:
		accRes(p, payload)
	case ROOTACC_REQ:
		rootAccRes(p, payload)
	case MINER_PING:
		pongRes(p, payload, MINER_PING)
	case CLIENT_PING:
		pongRes(p, payload, CLIENT_PING)
	case NEIGHBOR_REQ:
		neighborRes(p)
	case INTERMEDIATE_NODES_REQ:
		intermediateNodesRes(p, payload)

		//RESPONSES
	case NEIGHBOR_RES:
		if !peerSelfConn(p.getIPPort()) {
			processNeighborRes(p, payload)
		}
	case BLOCK_RES:
		forwardBlockReqToMiner(p, payload)
	case FUNDSTX_RES:
		forwardTxReqToMiner(p, payload, FUNDSTX_RES)
	case ACCTX_RES:
		forwardTxReqToMiner(p, payload, ACCTX_RES)
	case CONFIGTX_RES:
		forwardTxReqToMiner(p, payload, CONFIGTX_RES)
	case STAKETX_RES:
		forwardTxReqToMiner(p, payload, STAKETX_RES)
	case AGGTX_RES:
		forwardTxReqToMiner(p, payload, AGGTX_RES)
	case DELTX_RES:
		forwardTxReqToMiner(p, payload, DELTX_RES)
	}
}
