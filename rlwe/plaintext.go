package rlwe

import (
	"fmt"

	"github.com/tuneinsight/lattigo/v4/ring"
)

// Plaintext is a common base type for RLWE plaintexts.
type Plaintext struct {
	MetaData
	Value *ring.Poly
}

// NewPlaintext creates a new Plaintext at level `level` from the parameters.
func NewPlaintext(params Parameters, level int) (pt *Plaintext) {
	return &Plaintext{Value: ring.NewPoly(params.N(), level), MetaData: MetaData{Scale: params.defaultScale, IsNTT: params.defaultNTTFlag}}
}

// NewPlaintextAtLevelFromPoly constructs a new Plaintext at a specific level
// where the message is set to the passed poly. No checks are performed on poly and
// the returned Plaintext will share its backing array of coefficients.
// Returned plaintext's MetaData is empty.
func NewPlaintextAtLevelFromPoly(level int, poly *ring.Poly) (pt *Plaintext) {
	if len(poly.Coeffs) < level+1 {
		panic("cannot NewPlaintextAtLevelFromPoly: provided ring.Poly level is too small")
	}
	v0 := new(ring.Poly)
	v0.Coeffs = poly.Coeffs[:level+1]
	v0.Buff = poly.Buff[:poly.N()*(level+1)]
	return &Plaintext{Value: v0}
}

// Degree returns the degree of the target Plaintext.
func (pt *Plaintext) Degree() int {
	return 0
}

// Level returns the level of the target Plaintext.
func (pt *Plaintext) Level() int {
	return len(pt.Value.Coeffs) - 1
}

// GetScale gets the scale of the target Plaintext.
func (pt *Plaintext) GetScale() Scale {
	return pt.Scale
}

// SetScale sets the scale of the target Plaintext.
func (pt *Plaintext) SetScale(scale Scale) {
	pt.Scale = scale
}

// El returns the plaintext as a new `Element` for which the value points
// to the receiver `Value` field.
func (pt *Plaintext) El() *Ciphertext {
	return &Ciphertext{Value: []*ring.Poly{pt.Value}, MetaData: pt.MetaData}
}

// Copy copies the `other` plaintext value into the receiver plaintext.
func (pt *Plaintext) Copy(other *Plaintext) {
	if other != nil && other.Value != nil {
		pt.Value.Copy(other.Value)
		pt.MetaData = other.MetaData
	}
}

// MarshalBinarySize returns the size in bytes that the object once marshaled into a binary form.
func (pt *Plaintext) MarshalBinarySize() (dataLen int) {
	return pt.MetaData.MarshalBinarySize() + pt.Value.MarshalBinarySize64()
}

// MarshalBinary encodes the object into a binary form on a newly allocated slice of bytes.
func (pt *Plaintext) MarshalBinary() (data []byte, err error) {
	data = make([]byte, pt.MarshalBinarySize())
	_, err = pt.MarshalBinaryInPlace(data)
	return
}

// MarshalBinaryInPlace encodes the object into a binary form on a preallocated slice of bytes
// and returns the number of bytes written.
func (pt *Plaintext) MarshalBinaryInPlace(data []byte) (ptr int, err error) {

	if len(data) < pt.MarshalBinarySize() {
		return 0, fmt.Errorf("cannot write: len(data) is too small")
	}

	if ptr, err = pt.MetaData.MarshalBinaryInPlace(data); err != nil {
		return
	}

	var inc int
	if inc, err = pt.Value.Encode64(data[ptr:]); err != nil {
		return
	}

	ptr += inc

	return
}

// UnmarshalBinary decodes a slice of bytes generated by MarshalBinary
// or MarshalBinaryInPlace on the object.
func (pt *Plaintext) UnmarshalBinary(data []byte) (err error) {
	_, err = pt.UnmarshalBinaryInPlace(data)
	return
}

// UnmarshalBinaryInPlace decodes a slice of bytes generated by MarshalBinary or
// MarshalBinaryInPlace on the object and returns the number of bytes read.
func (pt *Plaintext) UnmarshalBinaryInPlace(data []byte) (ptr int, err error) {

	if ptr, err = pt.MetaData.UnmarshalBinaryInPlace(data); err != nil {
		return
	}

	if pt.Value == nil {
		pt.Value = new(ring.Poly)
	}

	var inc int
	if inc, err = pt.Value.Decode64(data[ptr:]); err != nil {
		return
	}

	ptr += inc

	return
}
