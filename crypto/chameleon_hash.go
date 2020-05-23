package crypto

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"os"
)

const HEX_BASE = 16
const LENGTH = 256 // Length of a single parameter in bits.

type ChameleonHashParameters struct {
	G  []byte // Prime
	P  []byte // Prime
	Q  []byte // Prime
	HK []byte // Public Hash Key
	TK []byte // Secret Trapdoor Key
}

type ChameleonHashCheckString struct {
	R []byte
	S []byte
}

// Generates a new set of chameleon hash parameters.
func newChameleonHashParameters() ChameleonHashParameters {
	var G, P, Q, HK, TK []byte
	keygen(LENGTH, &G, &P, &Q, &HK, &TK)

	return ChameleonHashParameters{
		G, P, Q, HK, TK,
	}
}

// Generates a new CheckString from the provided Q.
// Q is a parameter of a ChameleonHashParameters.
func NewChameleonHashCheckString(Q []byte) *ChameleonHashCheckString {
	var R, S []byte
	R = randgen(&Q)
	S = randgen(&Q)

	return &ChameleonHashCheckString{
		R: R,
		S: S,
	}
}

// Retrieve a set of chameleon hash parameters from file.
// Create if not exist.
func GetOrCreateChamHashParamsFromFile(filename string) (params *ChameleonHashParameters, err error) {

	// Create if not exists.
	if _, err = os.Stat(filename); os.IsNotExist(err) {
		params := newChameleonHashParameters()

		file, err := os.Create(filename)
		if err != nil {
			return &params, err
		}

		_, err = file.WriteString(string(params.G) + "\n")
		_, err = file.WriteString(string(params.P) + "\n")
		_, err = file.WriteString(string(params.Q) + "\n")
		_, err = file.WriteString(string(params.HK) + "\n")
		_, err = file.WriteString(string(params.TK) + "\n")
	}

	fileHandle, err := os.Open(filename)
	if err != nil {
		return params, errors.New(fmt.Sprintf("%v", err))
	}
	defer fileHandle.Close()

	scanner := bufio.NewScanner(fileHandle)

	gString := nextLine(scanner)
	pString := nextLine(scanner)
	qString := nextLine(scanner)
	hkString := nextLine(scanner)
	tkString := nextLine(scanner)

	if scanErr := scanner.Err(); scanErr != nil || err != nil {
		return params, errors.New(fmt.Sprintf("Could not read key from file: %v", err))
	}

	return &ChameleonHashParameters{
		G:  []byte(gString),
		P:  []byte(pString),
		Q:  []byte(qString),
		HK: []byte(hkString),
		TK: []byte(tkString),
	}, nil
}

// Returns a random hex number within the bounds of 0 and upperBoundHex.
func randgen(upperBoundHex *[]byte) []byte {
	upperBoundBig := new(big.Int)
	upperBoundBig, success := upperBoundBig.SetString(string(*upperBoundHex), HEX_BASE)
	if success != true {
		fmt.Printf("Conversion from hex: %s to bigInt failed.", upperBoundHex)
	}

	randomBig, err := rand.Int(rand.Reader, upperBoundBig)
	if err != nil {
		fmt.Printf("Generation of random bigInt in bounds [0...%v] failed.", upperBoundBig)
	}

	return []byte(fmt.Sprintf("%x", randomBig))
}

// Generates a set of chameleon hash keys of length LENGTH
func keygen(LENGTH int, G *[]byte, P *[]byte, Q *[]byte, HK *[]byte, TK *[]byte) {
	gBig := new(big.Int)
	qBig := new(big.Int)
	hkBig := new(big.Int)
	tkBig := new(big.Int)
	oneBig := new(big.Int)
	twoBig := new(big.Int)

	oneBig.SetInt64(1) // oneBig = 1
	twoBig.SetInt64(2) // twoBig = 2

	pBig, err := rand.Prime(rand.Reader, LENGTH) // pBig is a random prime of length LENGTH
	if err != nil {
		fmt.Printf("Generation of random prime number failed.")
	}
	qBig.Sub(pBig, oneBig) // qBig = pBig - 1
	qBig.Div(qBig, twoBig) // qBig = (pBig - 1) / 2

	gBig, err = rand.Int(rand.Reader, pBig)
	if err != nil {
		fmt.Printf("Generation of random bigInt in bounds [0...%v] failed.", pBig)
	}

	gBig.Exp(gBig, twoBig, pBig) // gBig = gBig ^ 2 % pBig

	// Choosing HK and TK
	tkBig, err = rand.Int(rand.Reader, qBig)
	if err != nil {
		fmt.Printf("Generation of random bigInt in bounds [0...%v] failed.", qBig)
	}

	hkBig.Exp(gBig, tkBig, pBig) // hkBig = gBig ^ tkBig % pBig

	*P = []byte(fmt.Sprintf("%x", pBig))
	*Q = []byte(fmt.Sprintf("%x", qBig))
	*G = []byte(fmt.Sprintf("%x", gBig))
	*HK = []byte(fmt.Sprintf("%x", hkBig))
	*TK = []byte(fmt.Sprintf("%x", tkBig))
}

// Returns the chameleon hash form a set of chameleon hash parameters, a check string and a message to hash.
func ChameleonHash(params *ChameleonHashParameters, checkString *ChameleonHashCheckString, message *[]byte) [32]byte {
	hkeBig := new(big.Int)
	gsBig := new(big.Int)
	tmpBig := new(big.Int)
	eBig := new(big.Int)
	pBig := new(big.Int)
	qBig := new(big.Int)
	gBig := new(big.Int)
	rBig := new(big.Int)
	sBig := new(big.Int)
	hkBig := new(big.Int)
	hBig := new(big.Int)

	// Converting from hex to bigInt
	gBig.SetString(string(params.G), HEX_BASE)
	pBig.SetString(string(params.P), HEX_BASE)
	qBig.SetString(string(params.Q), HEX_BASE)
	hkBig.SetString(string(params.HK), HEX_BASE)
	rBig.SetString(string(checkString.R), HEX_BASE)
	sBig.SetString(string(checkString.S), HEX_BASE)

	// Generate the hashOut with message || rBig
	hash := sha256.New()
	hash.Write(*message)
	hash.Write([]byte(fmt.Sprintf("%x", rBig)))

	eBig.SetBytes(hash.Sum(nil))

	hkeBig.Exp(hkBig, eBig, pBig)
	gsBig.Exp(gBig, sBig, pBig)
	tmpBig.Mul(hkeBig, gsBig)
	tmpBig.Mod(tmpBig, pBig)
	hBig.Sub(rBig, tmpBig)
	hBig.Mod(hBig, qBig)

	result := [32]byte{}
	copy(result[:], hBig.Bytes()) // Return hBig in big endian encoding as string

	return result
}

// Generates a hash collision for two different inputs (oldMessage, newMessage) and returns a new check string.
// ===== USAGE =====
// newCheckString := GenerateChamHashCollision(params, oldCheckString, oldMessage, newMessage)
// ChameleonHash(params, oldCheckString, oldMessage) == ChameleonHash(params, newCheckString, newMessage)
func GenerateChamHashCollision(
	params *ChameleonHashParameters,
	checkString *ChameleonHashCheckString,
	oldMessage *[]byte,
	newMessage *[]byte,
) *ChameleonHashCheckString {
	hkBig := new(big.Int)
	tkBig := new(big.Int)
	pBig := new(big.Int)
	qBig := new(big.Int)
	gBig := new(big.Int)
	r1Big := new(big.Int)
	s1Big := new(big.Int)
	kBig := new(big.Int)
	hBig := new(big.Int)
	eBig := new(big.Int)
	tmpBig := new(big.Int)
	r2Big := new(big.Int)
	s2Big := new(big.Int)

	gBig.SetString(string(params.G), HEX_BASE)
	pBig.SetString(string(params.P), HEX_BASE)
	qBig.SetString(string(params.Q), HEX_BASE)
	r1Big.SetString(string(checkString.R), HEX_BASE)
	s1Big.SetString(string(checkString.S), HEX_BASE)
	hkBig.SetString(string(params.HK), HEX_BASE)
	tkBig.SetString(string(params.TK), HEX_BASE)

	// Generate random k
	kBig, err := rand.Int(rand.Reader, qBig)
	if err != nil {
		fmt.Printf("Generation of random bigInt in bounds [0...%v] failed.", qBig)
	}

	// Get chameleon hash of the old message and old check-string
	hash := ChameleonHash(params, checkString, oldMessage)
	hBig.SetBytes(hash[:]) // Convert the big endian encoded hash into bigInt.

	// Compute the new r1
	tmpBig.Exp(gBig, kBig, pBig)
	r2Big.Add(hBig, tmpBig)
	r2Big.Mod(r2Big, qBig)

	// Compute e'
	newHash := sha256.New()
	newHash.Write([]byte(*newMessage))
	newHash.Write([]byte(fmt.Sprintf("%x", r2Big)))
	eBig.SetBytes(newHash.Sum(nil))

	// Compute s2
	tmpBig.Mul(eBig, tkBig)
	tmpBig.Mod(tmpBig, qBig)
	s2Big.Sub(kBig, tmpBig)
	s2Big.Mod(s2Big, qBig)

	return &ChameleonHashCheckString{
		R: []byte(fmt.Sprintf("%x", r2Big)),
		S: []byte(fmt.Sprintf("%x", s2Big)),
	}
}

func (params ChameleonHashParameters) String() string {
	return fmt.Sprintf("\n"+
		"G:   %s\n"+
		"P:   %s\n"+
		"Q:   %s\n"+
		"HK:  %s\n"+
		"TK:  %s\n",
		params.G, params.P, params.Q, params.HK, params.TK,
	)
}
