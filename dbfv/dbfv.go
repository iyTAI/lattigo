// Package dbfv implements a distributed (or threshold) version of the BFV scheme that
// enables secure multiparty computation solutions.
// See `drlwe/README.md` for additional information on multiparty schemes.
package dbfv

import (
	"github.com/tuneinsight/lattigo/v4/bfv"
	"github.com/tuneinsight/lattigo/v4/dbgv"
	"github.com/tuneinsight/lattigo/v4/drlwe"
	"github.com/tuneinsight/lattigo/v4/ring/distribution"
)

// NewPublicKeyGenProtocol creates a new drlwe.PublicKeyGenProtocol instance from the BFV parameters.
// The returned protocol instance is generic and can be used in other multiparty schemes.
func NewPublicKeyGenProtocol(params bfv.Parameters) *drlwe.PublicKeyGenProtocol {
	return drlwe.NewPublicKeyGenProtocol(params.Parameters)
}

// NewRelinKeyGenProtocol creates a new drlwe.RelinKeyGenProtocol instance from the BFV parameters.
// The returned protocol instance is generic and can be used in other multiparty schemes.
func NewRelinKeyGenProtocol(params bfv.Parameters) *drlwe.RelinKeyGenProtocol {
	return drlwe.NewRelinKeyGenProtocol(params.Parameters)
}

// NewGaloisKeyGenProtocol creates a new drlwe.RelinKeyGenProtocol instance from the BFV parameters.
// The returned protocol instance is generic and can be used in other multiparty schemes.
func NewGaloisKeyGenProtocol(params bfv.Parameters) *drlwe.GaloisKeyGenProtocol {
	return drlwe.NewGaloisKeyGenProtocol(params.Parameters)
}

// NewKeySwitchProtocol creates a new drlwe.KeySwitchProtocol instance from the BFV parameters.
// The returned protocol instance is generic and can be used in other multiparty schemes.
func NewKeySwitchProtocol(params bfv.Parameters, noise distribution.Distribution) *drlwe.KeySwitchProtocol {
	return drlwe.NewKeySwitchProtocol(params.Parameters, noise)
}

// NewPublicKeySwitchProtocol creates a new drlwe.PublicKeySwitchProtocol instance from the BFV paramters.
// The returned protocol instance is generic and can be used in other multiparty schemes.
func NewPublicKeySwitchProtocol(params bfv.Parameters, noise distribution.Distribution) *drlwe.PublicKeySwitchProtocol {
	return drlwe.NewPublicKeySwitchProtocol(params.Parameters, noise)
}

// NewRefreshProtocol creates a new instance of the RefreshProtocol.
func NewRefreshProtocol(params bfv.Parameters, noise distribution.Distribution) (rft *dbgv.RefreshProtocol) {
	return dbgv.NewRefreshProtocol(params.Parameters, noise)
}

// NewEncToShareProtocol creates a new instance of the EncToShareProtocol.
func NewEncToShareProtocol(params bfv.Parameters, noise distribution.Distribution) (e2s *dbgv.EncToShareProtocol) {
	return dbgv.NewEncToShareProtocol(bgv.Parameters(params), noise)
}

// NewShareToEncProtocol creates a new instance of the ShareToEncProtocol.
func NewShareToEncProtocol(params bfv.Parameters, noise distribution.Distribution) (e2s *dbgv.ShareToEncProtocol) {
	return dbgv.NewShareToEncProtocol(bgv.Parameters(params), noise)
}

// NewMaskedTransformProtocol creates a new instance of the MaskedTransformProtocol.
func NewMaskedTransformProtocol(paramsIn, paramsOut bfv.Parameters, noise distribution.Distribution) (rfp *dbgv.MaskedTransformProtocol, err error) {
	return dbgv.NewMaskedTransformProtocol(paramsIn.Parameters, paramsOut.Parameters, noise)
}
