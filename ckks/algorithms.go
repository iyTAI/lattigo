package ckks

import (
	"fmt"
	"math"

	"github.com/tuneinsight/lattigo/v4/rlwe"
)

// GoldschmidtDivisionNew homomorphically computes 1/x.
// input: ct: Enc(x) with values in the interval [0+minvalue, 2-minvalue] and logPrec the desired number of bits of precisions.
// output: Enc(1/x - e), where |e| <= (1-x)^2^(#iterations+1) -> the bit-precision doubles after each iteration.
// The method automatically estimates how many iterations are needed to achieve the desired precision, and returns an error if the input ciphertext
// does not have enough remaining level and if no bootstrapper was given.
func (eval *Evaluator) GoldschmidtDivisionNew(ct *rlwe.Ciphertext, minValue, logPrec float64, btp rlwe.Bootstrapper) (ctInv *rlwe.Ciphertext, err error) {

	parameters := eval.parameters

	start := math.Log2(1 - minValue)
	var iters int
	for start+logPrec > 0.5 {
		start *= 2 // Doubles the bit-precision at each iteration
		iters++
	}

	ptScale2ModuliRatio := parameters.PlaintextScaleToModuliRatio()

	if depth := iters * ptScale2ModuliRatio; btp == nil && depth > ct.Level() {
		return nil, fmt.Errorf("cannot GoldschmidtDivisionNew: ct.Level()=%d < depth=%d and rlwe.Bootstrapper is nil", ct.Level(), depth)
	}

	a := eval.MulNew(ct, -1)
	b := a.CopyNew()
	eval.Add(a, 2, a)
	eval.Add(b, 1, b)

	for i := 1; i < iters; i++ {

		if btp != nil && (b.Level() == btp.MinimumInputLevel() || b.Level() == ptScale2ModuliRatio-1) {
			if b, err = btp.Bootstrap(b); err != nil {
				return nil, err
			}
		}

		if btp != nil && (a.Level() == btp.MinimumInputLevel() || a.Level() == ptScale2ModuliRatio-1) {
			if a, err = btp.Bootstrap(a); err != nil {
				return nil, err
			}
		}

		eval.MulRelin(b, b, b)
		if err = eval.Rescale(b, parameters.PlaintextScale(), b); err != nil {
			return nil, err
		}

		if btp != nil && (b.Level() == btp.MinimumInputLevel() || b.Level() == ptScale2ModuliRatio-1) {
			if b, err = btp.Bootstrap(b); err != nil {
				return nil, err
			}
		}

		tmp := eval.MulRelinNew(a, b)
		if err = eval.Rescale(tmp, parameters.PlaintextScale(), tmp); err != nil {
			return nil, err
		}

		eval.SetScale(a, tmp.PlaintextScale)

		eval.Add(a, tmp, a)
	}

	return a, nil
}
