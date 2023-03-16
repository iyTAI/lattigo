package rlwe

import (
	"fmt"
)

// MetaData is a struct storing metadata.
type MetaData struct {
	Scale
	IsNTT        bool
	IsMontgomery bool
}

// Equal returns true if two MetaData structs are identical.
func (m *MetaData) Equal(other MetaData) (res bool) {
	res = m.Scale.Cmp(other.Scale) == 0
	res = res && m.IsNTT == other.IsNTT
	res = res && m.IsMontgomery == other.IsMontgomery
	return
}

// MarshalBinarySize returns the size in bytes that the object once marshalled into a binary form.
func (m *MetaData) MarshalBinarySize() int {
	return 2 + m.Scale.MarshalBinarySize()
}

// MarshalBinary encodes the object into a binary form on a newly allocated slice of bytes.
func (m *MetaData) MarshalBinary() (data []byte, err error) {
	data = make([]byte, m.MarshalBinarySize())
	_, err = m.MarshalBinaryInPlace(data)
	return
}

// UnmarshalBinary decodes a slice of bytes generated by MarshalBinary
// or MarshalBinaryInPlace on the object.
func (m *MetaData) UnmarshalBinary(data []byte) (err error) {
	_, err = m.UnmarshalBinaryInPlace(data)
	return
}

// MarshalBinaryInPlace encodes the object into a binary form on a preallocated slice of bytes
// and returns the number of bytes written.
func (m *MetaData) MarshalBinaryInPlace(data []byte) (ptr int, err error) {

	if len(data) < m.MarshalBinarySize() {
		return 0, fmt.Errorf("cannot write: len(data) is too small")
	}

	if ptr, err = m.Scale.MarshalBinaryInPlace(data[ptr:]); err != nil {
		return 0, err
	}

	if m.IsNTT {
		data[ptr] = 1
	}

	ptr++

	if m.IsMontgomery {
		data[ptr] = 1
	}

	ptr++

	return
}

// UnmarshalBinaryInPlace decodes a slice of bytes generated by MarshalBinary or
// MarshalBinaryInPlace on the object and returns the number of bytes read.
func (m *MetaData) UnmarshalBinaryInPlace(data []byte) (ptr int, err error) {

	if len(data) < m.MarshalBinarySize() {
		return 0, fmt.Errorf("canoot read: len(data) is too small")
	}

	if ptr, err = m.Scale.UnmarshalBinaryInPlace(data[ptr:]); err != nil {
		return
	}

	m.IsNTT = data[ptr] == 1
	ptr++

	m.IsMontgomery = data[ptr] == 1
	ptr++

	return
}
