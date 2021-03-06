package protocol

import (
	"bytes"
	"testing"
)

func TestVMContext_GetContractVariable_EncapsulationBreach(t *testing.T) {
	c := Context{}
	c.ContractVariables = []ByteArray{[]byte{0x00, 0x00, 0x00}}

	slice1, _ := c.GetContractVariable(0)

	for i := range slice1 {
		slice1[i] = 1
	}

	expected := []byte{0x00, 0x00, 0x00}
	actual, _ := c.GetContractVariable(0)
	if !bytes.Equal(expected, actual) {
		t.Errorf("Expected result to be '%v' but was '%v'", expected, actual)
	}

	expected = []byte{0x01, 0x01, 0x01}
	actual = slice1
	if !bytes.Equal(expected, actual) {
		t.Errorf("Expected result to be '%v' but was '%v'", expected, actual)
	}
}

func TestVMContext_SetContractVariable_EncapsulationBreach(t *testing.T) {
	c := Context{}
	c.ContractVariables = []ByteArray{[]byte{0x00, 0x00, 0x00}}

	slice1, _ := c.GetContractVariable(0)

	for i := range slice1 {
		slice1[i] = 1
	}

	c.SetContractVariable(0, slice1)
	c.PersistChanges()

	for i := range slice1 {
		slice1[i] = 2
	}

	expected := []byte{0x01, 0x01, 0x01}
	actual, _ := c.GetContractVariable(0)
	if !bytes.Equal(expected, actual) {
		t.Errorf("Expected result to be '%v' but was '%v'", expected, actual)
	}
}
