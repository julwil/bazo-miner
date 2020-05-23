package protocol

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"github.com/julwil/bazo-miner/crypto"
)

type Account struct {
	Address            [64]byte                        // 64 Byte
	Issuer             [32]byte                        // 32 Byte
	Balance            uint64                          // 8 Byte
	TxCnt              uint32                          // 4 Byte
	IsStaking          bool                            // 1 Byte
	CommitmentKey      [crypto.COMM_KEY_LENGTH]byte    // represents the modulus N of the RSA public key
	StakingBlockHeight uint32                          // 4 Byte
	Contract           []byte                          // Arbitrary length
	ContractVariables  []ByteArray                     // Arbitrary length
	ChamHashParams     *crypto.ChameleonHashParameters // Parameter set for chameleon hashing
}

func NewAccount(
	address [64]byte,
	issuer [32]byte,
	balance uint64,
	isStaking bool,
	commitmentKey [crypto.COMM_KEY_LENGTH]byte,
	contract []byte,
	contractVariables []ByteArray,
	chamHashParams *crypto.ChameleonHashParameters,
) Account {

	newAcc := Account{
		address,
		issuer,
		balance,
		0,
		isStaking,
		commitmentKey,
		0,
		contract,
		contractVariables,
		chamHashParams,
	}

	return newAcc
}

func (acc *Account) Hash() [32]byte {
	if acc == nil {
		return [32]byte{}
	}

	return SerializeHashContent(acc.Address)
}

func (acc *Account) Encode() []byte {
	if acc == nil {
		return nil
	}

	encoded := Account{
		Address:            acc.Address,
		Issuer:             acc.Issuer,
		Balance:            acc.Balance,
		TxCnt:              acc.TxCnt,
		IsStaking:          acc.IsStaking,
		CommitmentKey:      acc.CommitmentKey,
		StakingBlockHeight: acc.StakingBlockHeight,
		Contract:           acc.Contract,
		ContractVariables:  acc.ContractVariables,
		ChamHashParams:     acc.ChamHashParams,
	}

	buffer := new(bytes.Buffer)
	gob.NewEncoder(buffer).Encode(encoded)
	return buffer.Bytes()
}

func (*Account) Decode(encoded []byte) (acc *Account) {
	var decoded Account
	buffer := bytes.NewBuffer(encoded)
	decoder := gob.NewDecoder(buffer)
	decoder.Decode(&decoded)
	return &decoded
}

func (acc Account) String() string {
	addressHash := acc.Hash()
	return fmt.Sprintf(
		"Hash: %x, "+
			"Address: %x, "+
			"TxCnt: %v, "+
			"Balance: %v, "+
			"IsStaking: %v, ",
		addressHash[0:8],
		acc.Address[0:8],
		acc.TxCnt,
		acc.Balance,
		acc.IsStaking,
	)
}
