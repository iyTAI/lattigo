package bootstrapping

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tuneinsight/lattigo/v4/ckks"
	"github.com/tuneinsight/lattigo/v4/rlwe"
)

func BenchmarkBootstrap(b *testing.B) {

	var err error
	var btp *Bootstrapper

	paramSet := DefaultParametersDense[0]

	ckksParamsLit, btpParams, err := NewParametersFromLiteral(paramSet.SchemeParams, paramSet.BootstrappingParams)
	require.Nil(b, err)

	params, err := ckks.NewParametersFromLiteral(ckksParamsLit)
	if err != nil {
		panic(err)
	}

	kgen := ckks.NewKeyGenerator(params)
	sk := kgen.GenSecretKeyNew()

	evk := GenEvaluationKeySetNew(btpParams, params, sk)

	if btp, err = NewBootstrapper(params, btpParams, evk); err != nil {
		panic(err)
	}

	b.Run(ParamsToString(params, btpParams.PlaintextLogDimensions()[1], "Bootstrap/"), func(b *testing.B) {
		for i := 0; i < b.N; i++ {

			bootstrappingScale := rlwe.NewScale(math.Exp2(math.Round(math.Log2(float64(btp.params.Q()[0]) / btp.evalModPoly.MessageRatio()))))

			b.StopTimer()
			ct := ckks.NewCiphertext(params, 1, 0)
			ct.PlaintextScale = bootstrappingScale
			b.StartTimer()

			var t time.Time
			var ct0, ct1 *rlwe.Ciphertext

			// ModUp ct_{Q_0} -> ct_{Q_L}
			t = time.Now()
			ct = btp.modUpFromQ0(ct)
			b.Log("After ModUp  :", time.Since(t), ct.Level(), ct.PlaintextScale.Float64())

			//SubSum X -> (N/dslots) * Y^dslots
			t = time.Now()
			btp.Trace(ct, ct.PlaintextLogDimensions[1], ct)
			b.Log("After SubSum :", time.Since(t), ct.Level(), ct.PlaintextScale.Float64())

			// Part 1 : Coeffs to slots
			t = time.Now()
			ct0, ct1 = btp.CoeffsToSlotsNew(ct, btp.ctsMatrices)
			b.Log("After CtS    :", time.Since(t), ct0.Level(), ct0.PlaintextScale.Float64())

			// Part 2 : SineEval
			t = time.Now()
			ct0 = btp.EvalModNew(ct0, btp.evalModPoly)
			ct0.PlaintextScale = btp.params.PlaintextScale()

			if ct1 != nil {
				ct1 = btp.EvalModNew(ct1, btp.evalModPoly)
				ct1.PlaintextScale = btp.params.PlaintextScale()
			}
			b.Log("After Sine   :", time.Since(t), ct0.Level(), ct0.PlaintextScale.Float64())

			// Part 3 : Slots to coeffs
			t = time.Now()
			ct0 = btp.SlotsToCoeffsNew(ct0, ct1, btp.stcMatrices)
			ct0.PlaintextScale = rlwe.NewScale(math.Exp2(math.Round(math.Log2(ct0.PlaintextScale.Float64()))))
			b.Log("After StC    :", time.Since(t), ct0.Level(), ct0.PlaintextScale.Float64())
		}
	})
}
