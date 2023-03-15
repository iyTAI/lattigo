package rlwe

import (
	"encoding/binary"
	"fmt"
)

// GaloisKey is a type of evaluation key used to evaluate automorphisms on ciphertext.
// An automorphism pi: X^{i} -> X^{i*GaloisElement} changes the key under which the
// ciphertext is encrypted from s to pi(s). Thus, the ciphertext must be re-encrypted
// from pi(s) to s to ensure correctness, which is done with the corresponding GaloisKey.
//
// Lattigo implements automorphismes differently than the usual way (which is to first
// apply the automorphism and then the evaluation key). Instead the order of operations
// is reversed, the GaloisKey for pi^{-1} is evaluated on the ciphertext, outputing a
// ciphertext encrypted under pi^{-1}(s), and then the automorphism pi is applied. This
// enables a more efficient evaluation, by only having to apply the automorphism on the
// final result (instead of having to apply it on the decomposed ciphertext).
type GaloisKey struct {
	GaloisElement uint64
	NthRoot       uint64
	EvaluationKey
}

// NewGaloisKey allocates a new GaloisKey with zero coefficients and GaloisElement set to zero.
func NewGaloisKey(params Parameters) *GaloisKey {
	return &GaloisKey{EvaluationKey: *NewEvaluationKey(params, params.MaxLevelQ(), params.MaxLevelP()), NthRoot: params.RingQ().NthRoot()}
}

// Equals returns true if the two objects are equal.
func (gk *GaloisKey) Equals(other *GaloisKey) bool {
	return gk.EvaluationKey.Equals(&other.EvaluationKey) && gk.GaloisElement == other.GaloisElement && gk.NthRoot == other.NthRoot
}

// CopyNew creates a deep copy of the object and returns it
func (gk *GaloisKey) CopyNew() *GaloisKey {
	return &GaloisKey{
		GaloisElement: gk.GaloisElement,
		NthRoot:       gk.NthRoot,
		EvaluationKey: *gk.EvaluationKey.CopyNew(),
	}
}

// MarshalBinarySize returns the size in bytes that the object once marshaled into a binary form.
func (gk *GaloisKey) MarshalBinarySize() (dataLen int) {
	return gk.EvaluationKey.MarshalBinarySize() + 16
}

// MarshalBinary encodes the object into a binary form on a newly allocated slice of bytes.
func (gk *GaloisKey) MarshalBinary() (data []byte, err error) {
	data = make([]byte, gk.MarshalBinarySize())
	_, err = gk.MarshalBinaryInPlace(data)
	return
}

// MarshalBinaryInPlace encodes the object into a binary form on a preallocated slice of bytes
// and returns the number of bytes written.
func (gk *GaloisKey) MarshalBinaryInPlace(data []byte) (ptr int, err error) {

	if len(data) < 16 {
		return ptr, fmt.Errorf("cannot write: len(data) < 16")
	}

	binary.LittleEndian.PutUint64(data[ptr:], gk.GaloisElement)
	ptr += 8

	binary.LittleEndian.PutUint64(data[ptr:], gk.NthRoot)
	ptr += 8

	return gk.EvaluationKey.MarshalBinaryInPlace(data[ptr:])

}

// UnmarshalBinary decodes a slice of bytes generated by MarshalBinary
// or MarshalBinaryInPlace on the object.
func (gk *GaloisKey) UnmarshalBinary(data []byte) (err error) {
	_, err = gk.UnmarshalBinaryInPlace(data)
	return
}

// UnmarshalBinaryInPlace decodes a slice of bytes generated by MarshalBinary or
// MarshalBinaryInPlace on the object and returns the number of bytes read.
func (gk *GaloisKey) UnmarshalBinaryInPlace(data []byte) (ptr int, err error) {

	if len(data) < 16 {
		return ptr, fmt.Errorf("cannot read: len(data) < 16")
	}

	gk.GaloisElement = binary.LittleEndian.Uint64(data[ptr:])
	ptr += 8

	gk.NthRoot = binary.LittleEndian.Uint64(data[ptr:])
	ptr += 8

	var inc int
	if inc, err = gk.EvaluationKey.UnmarshalBinaryInPlace(data[ptr:]); err != nil {
		return
	}

	ptr += inc

	return
}
