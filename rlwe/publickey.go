package rlwe

import "github.com/tuneinsight/lattigo/v4/rlwe/ringqp"

// PublicKey is a type for generic RLWE public keys.
// The Value field stores the polynomials in NTT and Montgomery form.
type PublicKey struct {
	CiphertextQP
}

// NewPublicKey returns a new PublicKey with zero values.
func NewPublicKey(params Parameters) (pk *PublicKey) {
	return &PublicKey{CiphertextQP{Value: [2]ringqp.Poly{params.RingQP().NewPoly(), params.RingQP().NewPoly()}, MetaData: MetaData{IsNTT: true, IsMontgomery: true}}}
}

// LevelQ returns the level of the modulus Q of the target.
func (pk *PublicKey) LevelQ() int {
	return pk.Value[0].Q.Level()
}

// LevelP returns the level of the modulus P of the target.
// Returns -1 if P is absent.
func (pk *PublicKey) LevelP() int {
	if pk.Value[0].P != nil {
		return pk.Value[0].P.Level()
	}

	return -1
}

// Equals checks two PublicKey struct for equality.
func (pk *PublicKey) Equals(other *PublicKey) bool {
	if pk == other {
		return true
	}
	return pk.Value[0].Equals(other.Value[0]) && pk.Value[1].Equals(other.Value[1])
}

// CopyNew creates a deep copy of the receiver PublicKey and returns it.
func (pk *PublicKey) CopyNew() *PublicKey {
	if pk == nil {
		return nil
	}
	return &PublicKey{*pk.CiphertextQP.CopyNew()}
}

// MarshalBinarySize returns the size in bytes that the object once marshalled into a binary form.
func (pk *PublicKey) MarshalBinarySize() (dataLen int) {
	return pk.Value[0].MarshalBinarySize64() + pk.Value[1].MarshalBinarySize64() + pk.MetaData.MarshalBinarySize()
}

// MarshalBinary encodes the object into a binary form on a newly allocated slice of bytes.
func (pk *PublicKey) MarshalBinary() (data []byte, err error) {
	data = make([]byte, pk.MarshalBinarySize())
	if _, err = pk.MarshalBinaryInPlace(data); err != nil {
		return nil, err
	}
	return
}

// MarshalBinaryInPlace encodes the object into a binary form on a preallocated slice of bytes
// and returns the number of bytes written.
func (pk *PublicKey) MarshalBinaryInPlace(data []byte) (ptr int, err error) {
	return pk.CiphertextQP.MarshalBinaryInPlace(data)
}

// UnmarshalBinary decodes a slice of bytes generated by MarshalBinary
// or MarshalBinaryInPlace on the object.
func (pk *PublicKey) UnmarshalBinary(data []byte) (err error) {
	_, err = pk.UnmarshalBinaryInPlace(data)
	return
}

// UnmarshalBinaryInPlace decodes a slice of bytes generated by MarshalBinary or
// MarshalBinaryInPlace on the object and returns the number of bytes read.
func (pk *PublicKey) UnmarshalBinaryInPlace(data []byte) (ptr int, err error) {
	return pk.CiphertextQP.UnmarshalBinaryInPlace(data)
}
