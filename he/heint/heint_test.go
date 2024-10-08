package heint_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"runtime"
	"slices"
	"testing"

	"github.com/tuneinsight/lattigo/v5/core/rlwe"
	"github.com/tuneinsight/lattigo/v5/he/heint"
	"github.com/tuneinsight/lattigo/v5/ring"

	"github.com/stretchr/testify/require"
	"github.com/tuneinsight/lattigo/v5/utils/bignum"
	"github.com/tuneinsight/lattigo/v5/utils/sampling"
)

var flagPrintNoise = flag.Bool("print-noise", false, "print the residual noise")
var flagParamString = flag.String("params", "", "specify the test cryptographic parameters as a JSON string. Overrides -short.")

func GetTestName(opname string, p heint.Parameters, lvl int) string {
	return fmt.Sprintf("%s/LogN=%d/logQ=%d/logP=%d/LogSlots=%dx%d/logT=%d/Qi=%d/Pi=%d/lvl=%d",
		opname,
		p.LogN(),
		int(math.Round(p.LogQ())),
		int(math.Round(p.LogP())),
		p.LogMaxDimensions().Rows,
		p.LogMaxDimensions().Cols,
		int(math.Round(p.LogT())),
		p.QCount(),
		p.PCount(),
		lvl)
}

func TestInteger(t *testing.T) {

	var err error

	paramsLiterals := testParams

	if *flagParamString != "" {
		var jsonParams heint.ParametersLiteral
		if err = json.Unmarshal([]byte(*flagParamString), &jsonParams); err != nil {
			t.Fatal(err)
		}
		paramsLiterals = []heint.ParametersLiteral{jsonParams} // the custom test suite reads the parameters from the -params flag
	}

	for _, p := range paramsLiterals[:] {

		for _, plaintextModulus := range testPlaintextModulus[:] {

			p.PlaintextModulus = plaintextModulus

			var params heint.Parameters
			if params, err = heint.NewParametersFromLiteral(p); err != nil {
				t.Error(err)
				t.Fail()
			}

			var tc *testContext
			if tc, err = genTestParams(params); err != nil {
				t.Error(err)
				t.Fail()
			}

			for _, testSet := range []func(tc *testContext, t *testing.T){
				testLinearTransformation,
			} {
				testSet(tc, t)
				runtime.GC()
			}
		}
	}
}

type testContext struct {
	params      heint.Parameters
	ringQ       *ring.Ring
	ringT       *ring.Ring
	prng        sampling.PRNG
	uSampler    *ring.UniformSampler
	encoder     *heint.Encoder
	kgen        *rlwe.KeyGenerator
	sk          *rlwe.SecretKey
	pk          *rlwe.PublicKey
	encryptorPk *rlwe.Encryptor
	encryptorSk *rlwe.Encryptor
	decryptor   *rlwe.Decryptor
	evaluator   *heint.Evaluator
	testLevel   []int
}

func genTestParams(params heint.Parameters) (tc *testContext, err error) {

	tc = new(testContext)
	tc.params = params

	if tc.prng, err = sampling.NewPRNG(); err != nil {
		return nil, err
	}

	tc.ringQ = params.RingQ()
	tc.ringT = params.RingT()

	tc.uSampler = ring.NewUniformSampler(tc.prng, tc.ringT)
	tc.kgen = rlwe.NewKeyGenerator(tc.params)
	tc.sk, tc.pk = tc.kgen.GenKeyPairNew()
	tc.encoder = heint.NewEncoder(tc.params)

	tc.encryptorPk = rlwe.NewEncryptor(tc.params, tc.pk)
	tc.encryptorSk = rlwe.NewEncryptor(tc.params, tc.sk)
	tc.decryptor = rlwe.NewDecryptor(tc.params, tc.sk)
	tc.evaluator = heint.NewEvaluator(tc.params, rlwe.NewMemEvaluationKeySet(tc.kgen.GenRelinearizationKeyNew(tc.sk)))

	tc.testLevel = []int{0, params.MaxLevel()}

	return
}

func newTestVectorsLvl(level int, scale rlwe.Scale, tc *testContext, encryptor *rlwe.Encryptor) (coeffs ring.Poly, plaintext *rlwe.Plaintext, ciphertext *rlwe.Ciphertext) {
	coeffs = tc.uSampler.ReadNew()
	for i := range coeffs.Coeffs[0] {
		coeffs.Coeffs[0][i] = uint64(i)
	}

	plaintext = heint.NewPlaintext(tc.params, level)
	plaintext.Scale = scale
	tc.encoder.Encode(coeffs.Coeffs[0], plaintext)
	if encryptor != nil {
		var err error
		ciphertext, err = encryptor.EncryptNew(plaintext)
		if err != nil {
			panic(err)
		}
	}

	return coeffs, plaintext, ciphertext
}

func verifyTestVectors(tc *testContext, decryptor *rlwe.Decryptor, coeffs ring.Poly, element rlwe.ElementInterface[ring.Poly], t *testing.T) {

	coeffsTest := make([]uint64, tc.params.MaxSlots())

	switch el := element.(type) {
	case *rlwe.Plaintext:
		require.NoError(t, tc.encoder.Decode(el, coeffsTest))
	case *rlwe.Ciphertext:

		pt := decryptor.DecryptNew(el)

		require.NoError(t, tc.encoder.Decode(pt, coeffsTest))

		if *flagPrintNoise {
			require.NoError(t, tc.encoder.Encode(coeffsTest, pt))
			ct, err := tc.evaluator.SubNew(el, pt)
			require.NoError(t, err)
			vartmp, _, _ := rlwe.Norm(ct, decryptor)
			t.Logf("STD(noise): %f\n", vartmp)
		}

	default:
		t.Error("invalid test object to verify")
	}

	require.True(t, slices.Equal(coeffs.Coeffs[0], coeffsTest))
}

func testLinearTransformation(tc *testContext, t *testing.T) {

	rT := tc.params.RingT().SubRings[0]

	add := func(a, b, c []uint64) {
		rT.Add(a, b, c)
	}

	muladd := func(a, b, c []uint64) {
		rT.MulCoeffsBarrettThenAdd(a, b, c)
	}

	newVec := func(size int) (vec []uint64) {
		return make([]uint64, size)
	}

	params := tc.params

	T := params.PlaintextModulus()

	level := tc.params.MaxLevel()

	t.Run(GetTestName("Evaluator/LinearTransformationBSGS=true", params, level), func(t *testing.T) {

		values, _, ciphertext := newTestVectorsLvl(level, params.DefaultScale(), tc, tc.encryptorSk)

		slots := ciphertext.Slots()

		nonZeroDiags := []int{-15, -4, -1, 0, 1, 2, 3, 4, 15}

		diagonals := make(heint.Diagonals[uint64])
		for _, i := range nonZeroDiags {
			diagonals[i] = make([]uint64, slots)
			for j := 0; j < slots>>1; j++ {
				diagonals[i][j] = sampling.RandUint64() % T
			}
		}

		ltparams := heint.LinearTransformationParameters{
			DiagonalsIndexList:       diagonals.DiagonalsIndexList(),
			LevelQ:                   ciphertext.Level(),
			LevelP:                   params.MaxLevelP(),
			Scale:                    tc.params.DefaultScale(),
			LogDimensions:            ciphertext.LogDimensions,
			LogBabyStepGianStepRatio: 1,
		}

		// Allocate the linear transformation
		linTransf := heint.NewLinearTransformation(params, ltparams)

		// Encode on the linear transformation
		require.NoError(t, heint.EncodeLinearTransformation[uint64](tc.encoder, diagonals, linTransf))

		galEls := heint.GaloisElementsForLinearTransformation(params, ltparams)

		eval := tc.evaluator.WithKey(rlwe.NewMemEvaluationKeySet(nil, tc.kgen.GenGaloisKeysNew(galEls, tc.sk)...))
		ltEval := heint.NewLinearTransformationEvaluator(eval)

		require.NoError(t, ltEval.Evaluate(ciphertext, linTransf, ciphertext))

		values.Coeffs[0] = diagonals.Evaluate(values.Coeffs[0], newVec, add, muladd)

		verifyTestVectors(tc, tc.decryptor, values, ciphertext, t)
	})

	t.Run(GetTestName("Evaluator/LinearTransformationBSGS=false", params, level), func(t *testing.T) {

		values, _, ciphertext := newTestVectorsLvl(level, params.DefaultScale(), tc, tc.encryptorSk)

		slots := ciphertext.Slots()

		nonZeroDiags := []int{-15, -4, -1, 0, 1, 2, 3, 4, 15}

		diagonals := make(heint.Diagonals[uint64])
		for _, i := range nonZeroDiags {
			diagonals[i] = make([]uint64, slots)
			for j := 0; j < slots>>1; j++ {
				diagonals[i][j] = sampling.RandUint64() % T
			}
		}

		ltparams := heint.LinearTransformationParameters{
			DiagonalsIndexList:       diagonals.DiagonalsIndexList(),
			LevelQ:                   ciphertext.Level(),
			LevelP:                   params.MaxLevelP(),
			Scale:                    params.DefaultScale(),
			LogDimensions:            ciphertext.LogDimensions,
			LogBabyStepGianStepRatio: -1,
		}

		// Allocate the linear transformation
		linTransf := heint.NewLinearTransformation(params, ltparams)

		// Encode on the linear transformation
		require.NoError(t, heint.EncodeLinearTransformation[uint64](tc.encoder, diagonals, linTransf))

		galEls := heint.GaloisElementsForLinearTransformation(params, ltparams)

		eval := tc.evaluator.WithKey(rlwe.NewMemEvaluationKeySet(nil, tc.kgen.GenGaloisKeysNew(galEls, tc.sk)...))
		ltEval := heint.NewLinearTransformationEvaluator(eval)

		require.NoError(t, ltEval.Evaluate(ciphertext, linTransf, ciphertext))

		values.Coeffs[0] = diagonals.Evaluate(values.Coeffs[0], newVec, add, muladd)

		verifyTestVectors(tc, tc.decryptor, values, ciphertext, t)
	})

	t.Run(GetTestName("Evaluator/LinearTransformation/Permutation", params, level), func(t *testing.T) {

		idx := [2][]int{
			make([]int, params.MaxSlots()>>1),
			make([]int, params.MaxSlots()>>1),
		}

		for i := range idx[0] {
			idx[0][i] = i
			idx[1][i] = i
		}

		r := rand.New(rand.NewSource(0))
		r.Shuffle(len(idx[0]), func(i, j int) {
			idx[0][i], idx[0][j] = idx[0][j], idx[0][i]
		})
		r.Shuffle(len(idx[1]), func(i, j int) {
			idx[1][i], idx[1][j] = idx[1][j], idx[1][i]
		})

		idx[0] = idx[0][:len(idx[0])>>1]
		idx[1] = idx[1][:len(idx[1])>>1] // truncates to test partial permutation

		permutation := [2][]heint.PermutationMapping[uint64]{
			make([]heint.PermutationMapping[uint64], len(idx[0])),
			make([]heint.PermutationMapping[uint64], len(idx[1])),
		}

		for i := range permutation {
			for j := range permutation[i] {
				permutation[i][j] = heint.PermutationMapping[uint64]{
					From:    j,
					To:      idx[i][j],
					Scaling: sampling.RandUint64() % T,
				}
				permutation[i][j] = heint.PermutationMapping[uint64]{
					From:    j,
					To:      idx[i][j],
					Scaling: sampling.RandUint64() % T,
				}
			}
		}

		diagonals := heint.Permutation[uint64](permutation).GetDiagonals(params.LogMaxSlots())

		values, _, ciphertext := newTestVectorsLvl(level, tc.params.NewScale(1), tc, tc.encryptorSk)

		ltparams := heint.LinearTransformationParameters{
			DiagonalsIndexList:       diagonals.DiagonalsIndexList(),
			LevelQ:                   ciphertext.Level(),
			LevelP:                   params.MaxLevelP(),
			Scale:                    params.DefaultScale(),
			LogDimensions:            ciphertext.LogDimensions,
			LogBabyStepGianStepRatio: 1,
		}

		// Allocate the linear transformation
		linTransf := heint.NewLinearTransformation(params, ltparams)

		// Encode on the linear transformation
		require.NoError(t, heint.EncodeLinearTransformation[uint64](tc.encoder, diagonals, linTransf))

		galEls := heint.GaloisElementsForLinearTransformation(params, ltparams)

		evk := rlwe.NewMemEvaluationKeySet(nil, tc.kgen.GenGaloisKeysNew(galEls, tc.sk)...)

		ltEval := heint.NewLinearTransformationEvaluator(tc.evaluator.WithKey(evk))

		require.NoError(t, ltEval.Evaluate(ciphertext, linTransf, ciphertext))

		values.Coeffs[0] = diagonals.Evaluate(values.Coeffs[0], newVec, add, muladd)

		verifyTestVectors(tc, tc.decryptor, values, ciphertext, t)
	})

	t.Run("Evaluator/PolyEval", func(t *testing.T) {

		t.Run("Single", func(t *testing.T) {

			if tc.params.MaxLevel() < 4 {
				t.Skip("MaxLevel() to low")
			}

			values, _, ciphertext := newTestVectorsLvl(tc.params.MaxLevel(), tc.params.NewScale(1), tc, tc.encryptorSk)

			coeffs := []uint64{0, 0, 1}

			T := tc.params.PlaintextModulus()
			for i := range values.Coeffs[0] {
				values.Coeffs[0][i] = ring.EvalPolyModP(values.Coeffs[0][i], coeffs, T)
			}

			poly := bignum.NewPolynomial(bignum.Monomial, coeffs, nil)

			t.Run(GetTestName("Standard", tc.params, tc.params.MaxLevel()), func(t *testing.T) {

				polyEval := heint.NewPolynomialEvaluator(tc.params, tc.evaluator, false)

				res, err := polyEval.Evaluate(ciphertext, poly, tc.params.DefaultScale())
				require.NoError(t, err)

				require.Equal(t, res.Scale.Cmp(tc.params.DefaultScale()), 0)

				verifyTestVectors(tc, tc.decryptor, values, res, t)
			})

			t.Run(GetTestName("Invariant", tc.params, tc.params.MaxLevel()), func(t *testing.T) {

				polyEval := heint.NewPolynomialEvaluator(tc.params, tc.evaluator, true)

				res, err := polyEval.Evaluate(ciphertext, poly, tc.params.DefaultScale())
				require.NoError(t, err)

				require.Equal(t, res.Level(), ciphertext.Level())
				require.Equal(t, res.Scale.Cmp(tc.params.DefaultScale()), 0)

				verifyTestVectors(tc, tc.decryptor, values, res, t)
			})
		})

		t.Run("Vector", func(t *testing.T) {

			if tc.params.MaxLevel() < 4 {
				t.Skip("MaxLevel() to low")
			}

			values, _, ciphertext := newTestVectorsLvl(tc.params.MaxLevel(), tc.params.NewScale(7), tc, tc.encryptorSk)

			coeffs0 := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
			coeffs1 := []uint64{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}

			slots := values.N()

			mapping := make(map[int][]int)
			idx0 := make([]int, slots>>1)
			idx1 := make([]int, slots>>1)
			for i := 0; i < slots>>1; i++ {
				idx0[i] = 2 * i
				idx1[i] = 2*i + 1
			}

			mapping[0] = idx0
			mapping[1] = idx1

			polyVector, err := heint.NewPolynomialVector([][]uint64{
				coeffs0,
				coeffs1,
			}, mapping)
			require.NoError(t, err)

			TInt := new(big.Int).SetUint64(tc.params.PlaintextModulus())
			for pol, idx := range mapping {
				for _, i := range idx {
					values.Coeffs[0][i] = polyVector.Value[pol].EvaluateModP(new(big.Int).SetUint64(values.Coeffs[0][i]), TInt).Uint64()
				}
			}

			t.Run(GetTestName("Standard", tc.params, tc.params.MaxLevel()), func(t *testing.T) {

				polyEval := heint.NewPolynomialEvaluator(tc.params, tc.evaluator, false)

				res, err := polyEval.Evaluate(ciphertext, polyVector, tc.params.DefaultScale())
				require.NoError(t, err)

				require.Equal(t, res.Scale.Cmp(tc.params.DefaultScale()), 0)

				verifyTestVectors(tc, tc.decryptor, values, res, t)
			})

			t.Run(GetTestName("Invariant", tc.params, tc.params.MaxLevel()), func(t *testing.T) {

				polyEval := heint.NewPolynomialEvaluator(tc.params, tc.evaluator, true)

				res, err := polyEval.Evaluate(ciphertext, polyVector, tc.params.DefaultScale())
				require.NoError(t, err)

				require.Equal(t, res.Level(), ciphertext.Level())
				require.Equal(t, res.Scale.Cmp(tc.params.DefaultScale()), 0)

				verifyTestVectors(tc, tc.decryptor, values, res, t)
			})
		})
	})

}
