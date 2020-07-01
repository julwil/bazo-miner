package protocol

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/gob"
	"fmt"
	"github.com/julwil/bazo-miner/crypto"
	"golang.org/x/crypto/sha3"
)

const UPDATE_TX_SIZE = 42

type UpdateTx struct {
	Header                        byte
	Fee                           uint64
	TxToUpdateHash                [32]byte                         // Hash of the tx to be updated.
	TxToUpdateChamHashCheckString *crypto.ChameleonHashCheckString // New Chameleon hash check string for the tx to be updated.
	TxToUpdateData                []byte                           // Holds the data to be updated on the TxToUpdate's data field.
	Issuer                        [32]byte                         // Address of the issuer of the update request.
	Sig                           [64]byte                         // Signature of the issuer of the update request.
	ChamHashCheckString           *crypto.ChameleonHashCheckString // Chameleon hash check string associated with this tx.
	Data                          []byte                           // Data field for user-related data.
}

func ConstrUpdateTx(
	header byte,
	fee uint64,
	txToUpdateHash [32]byte,
	txToUpdateChamHashCheckString *crypto.ChameleonHashCheckString,
	txToUpdateData []byte,
	issuer [32]byte,
	privateKey *ecdsa.PrivateKey,
	chCheckString *crypto.ChameleonHashCheckString,
	chParams *crypto.ChameleonHashParameters,
	data []byte,
) (tx *UpdateTx, err error) {
	tx = new(UpdateTx)
	tx.Header = header
	tx.Fee = fee
	tx.TxToUpdateHash = txToUpdateHash
	tx.TxToUpdateChamHashCheckString = txToUpdateChamHashCheckString
	tx.TxToUpdateData = txToUpdateData
	tx.Issuer = issuer
	tx.ChamHashCheckString = chCheckString
	tx.Data = data

	// Generate the hash of the new Tx
	txHash := tx.ChameleonHash(chParams)

	// Sign the Tx
	r, s, err := ecdsa.Sign(rand.Reader, privateKey, txHash[:])
	if err != nil {
		return nil, err
	}
	copy(tx.Sig[32-len(r.Bytes()):32], r.Bytes())
	copy(tx.Sig[64-len(s.Bytes()):], s.Bytes())

	return tx, err
}

// Returns SHA3 hash over the tx content
func (tx *UpdateTx) SHA3() [32]byte {
	toHash := struct {
		Header                        byte
		Fee                           uint64
		TxToUpdateHash                [32]byte
		TxToUpdateChamHashCheckString crypto.ChameleonHashCheckString
		TxToUpdateData                []byte
		Issuer                        [32]byte
		ChamHashCheckString           crypto.ChameleonHashCheckString
		Data                          []byte
	}{
		tx.Header,
		tx.Fee,
		tx.TxToUpdateHash,
		*tx.TxToUpdateChamHashCheckString,
		tx.TxToUpdateData,
		tx.Issuer,
		*tx.ChamHashCheckString,
		tx.Data,
	}

	return sha3.Sum256([]byte(fmt.Sprintf("%v", toHash)))
}

func (tx *UpdateTx) Hash() (hash [32]byte) {
	if tx == nil {
		return [32]byte{}
	}

	chParams := crypto.ChParamsMap[tx.Issuer]

	return tx.ChameleonHash(chParams)
}

// Returns the chameleon hash but takes the chameleon hash parameters as input.
// This method should be called in the context of bazo-client as the client doesn't maintain
// a state holding the chameleon hash parameters of each account.
func (tx *UpdateTx) ChameleonHash(chParams *crypto.ChameleonHashParameters) [32]byte {
	sha3Hash := tx.SHA3()
	hashInput := sha3Hash[:]

	return crypto.ChameleonHash(chParams, tx.ChamHashCheckString, &hashInput)
}

func (tx *UpdateTx) Encode() (encodedTx []byte) {
	encodeData := UpdateTx{
		Header:                        tx.Header,
		Fee:                           tx.Fee,
		TxToUpdateHash:                tx.TxToUpdateHash,
		TxToUpdateChamHashCheckString: tx.TxToUpdateChamHashCheckString,
		TxToUpdateData:                tx.TxToUpdateData,
		Issuer:                        tx.Issuer,
		Sig:                           tx.Sig,
		ChamHashCheckString:           tx.ChamHashCheckString,
		Data:                          tx.Data,
	}
	buffer := new(bytes.Buffer)
	gob.NewEncoder(buffer).Encode(encodeData)

	return buffer.Bytes()
}

func (*UpdateTx) Decode(encodedTx []byte) *UpdateTx {
	var decoded UpdateTx
	buffer := bytes.NewBuffer(encodedTx)
	decoder := gob.NewDecoder(buffer)
	decoder.Decode(&decoded)

	return &decoded
}

func (tx *UpdateTx) TxFee() uint64 { return tx.Fee }

func (tx *UpdateTx) Size() uint64 { return UPDATE_TX_SIZE }

func (tx *UpdateTx) Sender() [32]byte { return tx.Issuer }

func (tx *UpdateTx) Receiver() [32]byte { return tx.Issuer }

func (tx UpdateTx) String() string {

	return fmt.Sprintf(
		"\nHeader: %v\n"+
			"Fee: %v\n"+
			"TxToUpdate: %x\n"+
			"TxToUpdateChamHashCheckString: %x\n"+
			"TxToUpdateData: %s\n"+
			"Issuer: %x\n"+
			"Sig: %x\n"+
			"ChCheckString: %x\n"+
			"Data: %s",
		tx.Header,
		tx.Fee,
		tx.TxToUpdateHash,
		tx.TxToUpdateChamHashCheckString.R[0:8],
		tx.TxToUpdateData,
		tx.Issuer[0:8],
		tx.Sig[0:8],
		tx.ChamHashCheckString.R[0:8],
		tx.Data,
	)
}

func (tx *UpdateTx) SetData(data []byte) {
	tx.Data = data
}

func (tx *UpdateTx) GetData() []byte {
	return tx.Data
}

func (tx *UpdateTx) SetChCheckString(checkString *crypto.ChameleonHashCheckString) {
	tx.ChamHashCheckString = checkString
}

func (tx *UpdateTx) GetChCheckString() *crypto.ChameleonHashCheckString {
	return tx.ChamHashCheckString
}
