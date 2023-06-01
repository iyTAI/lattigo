package bgv

import (
	"fmt"
	"math"
	"math/big"

	"github.com/tuneinsight/lattigo/v4/ring"
	"github.com/tuneinsight/lattigo/v4/rlwe"
	"github.com/tuneinsight/lattigo/v4/rlwe/ringqp"
	"github.com/tuneinsight/lattigo/v4/utils"
)

// Evaluator is a struct that holds the necessary elements to perform the homomorphic operations between ciphertexts and/or plaintexts.
// It also holds a memory buffer used to store intermediate computations.
type Evaluator struct {
	*evaluatorBase
	*evaluatorBuffers
	*rlwe.Evaluator
	*Encoder
}

type evaluatorBase struct {
	tMontgomery         ring.RNSScalar
	levelQMul           []int      // optimal #QiMul depending on #Qi (variable level)
	pHalf               []*big.Int // all prod(QiMul) / 2 depending on #Qi
	basisExtenderQ1toQ2 *ring.BasisExtender
}

func newEvaluatorPrecomp(parameters Parameters) *evaluatorBase {
	ringQ := parameters.RingQ()
	ringQMul := parameters.RingQMul()
	t := parameters.T()

	levelQMul := make([]int, ringQ.ModuliChainLength())
	Q := new(big.Int).SetUint64(1)
	for i := range levelQMul {
		Q.Mul(Q, new(big.Int).SetUint64(ringQ.SubRings[i].Modulus))
		levelQMul[i] = int(math.Ceil(float64(Q.BitLen()+parameters.LogN())/61.0)) - 1
	}

	pHalf := make([]*big.Int, ringQMul.ModuliChainLength())

	QMul := new(big.Int).SetUint64(1)
	for i := range pHalf {
		QMul.Mul(QMul, new(big.Int).SetUint64(ringQMul.SubRings[i].Modulus))
		pHalf[i] = new(big.Int).Rsh(QMul, 1)
	}

	basisExtenderQ1toQ2 := ring.NewBasisExtender(ringQ, ringQMul)

	// T * 2^{64} mod Q
	tMontgomery := ringQ.NewRNSScalarFromBigint(new(big.Int).Lsh(new(big.Int).SetUint64(t), 64))
	ringQ.MFormRNSScalar(tMontgomery, tMontgomery)

	return &evaluatorBase{
		tMontgomery:         tMontgomery,
		levelQMul:           levelQMul,
		pHalf:               pHalf,
		basisExtenderQ1toQ2: basisExtenderQ1toQ2,
	}
}

type evaluatorBuffers struct {
	buffQ    [3]*ring.Poly
	buffQMul [9]*ring.Poly
}

// BuffQ returns a pointer to the internal memory buffer buffQ.
func (eval *Evaluator) BuffQ() [3]*ring.Poly {
	return eval.buffQ
}

// GetRLWEEvaluator returns the underlying *rlwe.Evaluator.
func (eval *Evaluator) GetRLWEEvaluator() *rlwe.Evaluator {
	return eval.Evaluator
}

func newEvaluatorBuffer(params Parameters) *evaluatorBuffers {

	ringQ := params.RingQ()
	buffQ := [3]*ring.Poly{
		ringQ.NewPoly(),
		ringQ.NewPoly(),
		ringQ.NewPoly(),
	}

	ringQMul := params.RingQMul()

	buffQMul := [9]*ring.Poly{
		ringQMul.NewPoly(),
		ringQMul.NewPoly(),
		ringQMul.NewPoly(),
		ringQMul.NewPoly(),
		ringQMul.NewPoly(),
		ringQMul.NewPoly(),
		ringQMul.NewPoly(),
		ringQMul.NewPoly(),
		ringQMul.NewPoly(),
	}

	return &evaluatorBuffers{
		buffQ:    buffQ,
		buffQMul: buffQMul,
	}
}

// NewEvaluator creates a new Evaluator, that can be used to do homomorphic
// operations on ciphertexts and/or plaintexts. It stores a memory buffer
// and ciphertexts that will be used for intermediate values.
func NewEvaluator(parameters Parameters, evk rlwe.EvaluationKeySetInterface) *Evaluator {
	ev := new(Evaluator)
	ev.evaluatorBase = newEvaluatorPrecomp(parameters)
	ev.evaluatorBuffers = newEvaluatorBuffer(parameters)
	ev.Evaluator = rlwe.NewEvaluator(parameters, evk)
	ev.Encoder = NewEncoder(parameters)

	return ev
}

// Parameters returns the Parameters of the underlying struct as an rlwe.ParametersInterface.
func (eval *Evaluator) Parameters() rlwe.ParametersInterface {
	return eval.parameters
}

// ShallowCopy creates a shallow copy of this Evaluator in which the read-only data-structures are
// shared with the receiver.
func (eval *Evaluator) ShallowCopy() *Evaluator {
	return &Evaluator{
		evaluatorBase:    eval.evaluatorBase,
		Evaluator:        eval.Evaluator.ShallowCopy(),
		evaluatorBuffers: newEvaluatorBuffer(eval.Parameters().(Parameters)),
	}
}

// WithKey creates a shallow copy of this Evaluator in which the read-only data-structures are
// shared with the receiver but the EvaluationKey is evaluationKey.
func (eval *Evaluator) WithKey(evk rlwe.EvaluationKeySetInterface) *Evaluator {
	return &Evaluator{
		evaluatorBase:    eval.evaluatorBase,
		Evaluator:        eval.Evaluator.WithKey(evk),
		evaluatorBuffers: eval.evaluatorBuffers,
	}
}

func (eval *Evaluator) evaluateInPlace(level int, el0 *rlwe.Ciphertext, el1 *rlwe.OperandQ, elOut *rlwe.Ciphertext, evaluate func(*ring.Poly, *ring.Poly, *ring.Poly)) {

	smallest, largest, _ := rlwe.GetSmallestLargest(el0.El(), el1.El())

	elOut.Resize(utils.Max(el0.Degree(), el1.Degree()), level)

	for i := 0; i < smallest.Degree()+1; i++ {
		evaluate(el0.Value[i], el1.Value[i], elOut.Value[i])
	}

	// If the inputs degrees differ, it copies the remaining degree on the receiver.
	if largest != nil && largest != elOut.El() { // checks to avoid unnecessary work.
		for i := smallest.Degree() + 1; i < largest.Degree()+1; i++ {
			elOut.Value[i].Copy(largest.Value[i])
		}
	}

	elOut.MetaData = el0.MetaData
}

func (eval *Evaluator) matchScaleThenEvaluateInPlace(level int, el0 *rlwe.Ciphertext, el1 *rlwe.OperandQ, elOut *rlwe.Ciphertext, evaluate func(*ring.Poly, uint64, *ring.Poly)) {

	elOut.Resize(utils.Max(el0.Degree(), el1.Degree()), level)

	r0, r1, _ := eval.matchScalesBinary(el0.PlaintextScale.Uint64(), el1.PlaintextScale.Uint64())

	for i := range el0.Value {
		eval.parameters.RingQ().AtLevel(level).MulScalar(el0.Value[i], r0, elOut.Value[i])
	}

	for i := el0.Degree(); i < elOut.Degree(); i++ {
		elOut.Value[i].Zero()
	}

	for i := range el1.Value {
		evaluate(el1.Value[i], r1, elOut.Value[i])
	}

	elOut.MetaData = el0.MetaData
	elOut.PlaintextScale = el0.PlaintextScale.Mul(eval.parameters.NewScale(r0))
}

func (eval *Evaluator) newCiphertextBinary(op0, op1 rlwe.Operand) (ctOut *rlwe.Ciphertext) {
	return NewCiphertext(eval.parameters, utils.Max(op0.Degree(), op1.Degree()), utils.Min(op0.Level(), op1.Level()))
}

// Add adds op1 to op0 and returns the result in op2.
func (eval *Evaluator) Add(op0 *rlwe.Ciphertext, op1 interface{}, op2 *rlwe.Ciphertext) {

	ringQ := eval.parameters.RingQ()

	switch op1 := op1.(type) {
	case rlwe.Operand:

		_, level := eval.CheckBinary(op0.El(), op1.El(), op2.El(), utils.Max(op0.Degree(), op1.Degree()))

		if op0.PlaintextScale.Cmp(op1.El().PlaintextScale) == 0 {
			eval.evaluateInPlace(level, op0, op1.El(), op2, ringQ.AtLevel(level).Add)
		} else {
			eval.matchScaleThenEvaluateInPlace(level, op0, op1.El(), op2, ringQ.AtLevel(level).MulScalarThenAdd)
		}

	case uint64:

		ringT := eval.parameters.RingT()

		_, level := eval.CheckUnary(op0.El(), op2.El())

		op2.Resize(op0.Degree(), level)

		if op0.PlaintextScale.Cmp(eval.parameters.NewScale(1)) != 0 {
			op1 = ring.BRed(op1, op0.PlaintextScale.Uint64(), ringT.SubRings[0].Modulus, ringT.SubRings[0].BRedConstant)
		} else {
			op1 = ring.BRedAdd(op1, ringT.SubRings[0].Modulus, ringT.SubRings[0].BRedConstant)
		}

		// Scales the scalar to the scale of op0
		op1Big := new(big.Int).SetUint64(op1)
		op1Big.Mul(op1Big, eval.tInvModQ[level])

		ringQ.AtLevel(level).AddScalarBigint(op0.Value[0], op1Big, op2.Value[0])

		if op0 != op2 {
			for i := 1; i < op0.Degree()+1; i++ {
				ring.Copy(op0.Value[i], op2.Value[i])
			}

			op2.MetaData = op0.MetaData
		}
	case []uint64:

		// Retrieves minimum level
		level := utils.Min(op0.Level(), op2.Level())

		// Resizes output to minimum level
		op2.Resize(op0.Degree(), level)

		// Instantiates new plaintext from buffer
		pt := rlwe.NewPlaintextAtLevelFromPoly(level, eval.buffQ[0])
		pt.MetaData = op0.MetaData // Sets the metadata, notably matches scalses

		// Encodes the vector on the plaintext
		if err := eval.Encoder.Encode(op1, pt); err != nil {
			panic(err)
		}

		// Generic in place evaluation
		eval.evaluateInPlace(level, op0, pt.El(), op2, eval.parameters.RingQ().AtLevel(level).Add)
	default:
		panic(fmt.Sprintf("invalid op1.(Type), expected rlwe.Operand, []uint64 or or uint64, but got %T", op1))
	}
}

// AddNew adds op1 to op0 and returns the result in a new op2.
func (eval *Evaluator) AddNew(op0 *rlwe.Ciphertext, op1 interface{}) (op2 *rlwe.Ciphertext) {

	switch op1 := op1.(type) {
	case rlwe.Operand:
		op2 = eval.newCiphertextBinary(op0, op1)
	default:
		op2 = NewCiphertext(eval.parameters, op0.Degree(), op0.Level())
		op2.MetaData = op0.MetaData
	}

	eval.Add(op0, op1, op2)
	return
}

// Sub subtracts op1 to op0 and returns the result in op2.
func (eval *Evaluator) Sub(op0 *rlwe.Ciphertext, op1 interface{}, op2 *rlwe.Ciphertext) {

	switch op1 := op1.(type) {
	case rlwe.Operand:

		_, level := eval.CheckBinary(op0.El(), op1.El(), op2.El(), utils.Max(op0.Degree(), op1.Degree()))

		ringQ := eval.parameters.RingQ()

		if op0.PlaintextScale.Cmp(op1.El().PlaintextScale) == 0 {
			eval.evaluateInPlace(level, op0, op1.El(), op2, ringQ.AtLevel(level).Sub)
		} else {
			eval.matchScaleThenEvaluateInPlace(level, op0, op1.El(), op2, ringQ.AtLevel(level).MulScalarThenSub)
		}
	case uint64:
		T := eval.parameters.T()
		eval.Add(op0, T-(op1%T), op2)
	case []uint64:

		// Retrieves minimum level
		level := utils.Min(op0.Level(), op2.Level())

		// Resizes output to minimum level
		op2.Resize(op0.Degree(), level)

		// Instantiates new plaintext from buffer
		pt := rlwe.NewPlaintextAtLevelFromPoly(level, eval.buffQ[0])
		pt.MetaData = op0.MetaData // Sets the metadata, notably matches scalses

		// Encodes the vector on the plaintext
		if err := eval.Encoder.Encode(op1, pt); err != nil {
			panic(err)
		}

		// Generic in place evaluation
		eval.evaluateInPlace(level, op0, pt.El(), op2, eval.parameters.RingQ().AtLevel(level).Sub)
	default:
		panic(fmt.Sprintf("invalid op1.(Type), expected rlwe.Operand, []uint64 or uint64, but got %T", op1))
	}
}

// SubNew subtracts op1 to op0 and returns the result in a new ctOut.
func (eval *Evaluator) SubNew(op0 *rlwe.Ciphertext, op1 interface{}) (op2 *rlwe.Ciphertext) {
	switch op1 := op1.(type) {
	case rlwe.Operand:
		op2 = eval.newCiphertextBinary(op0, op1)
	default:
		op2 = NewCiphertext(eval.parameters, op0.Degree(), op0.Level())
		op2.MetaData = op0.MetaData
	}
	eval.Sub(op0, op1, op2)
	return
}

// Neg negates ctIn and returns the result in ctOut.
func (eval *Evaluator) Neg(ctIn *rlwe.Ciphertext, ctOut *rlwe.Ciphertext) {

	if ctIn.Degree() != ctOut.Degree() {
		panic("cannot Negate: invalid receiver Ciphertext does not match input Ciphertext degree")
	}

	level := utils.Min(ctIn.Level(), ctOut.Level())

	for i := range ctIn.Value {
		eval.parameters.RingQ().AtLevel(level).Neg(ctIn.Value[i], ctOut.Value[i])
	}

	ctOut.MetaData = ctIn.MetaData
}

// NegNew negates ctIn and returns the result in a new ctOut.
func (eval *Evaluator) NegNew(ctIn *rlwe.Ciphertext) (ctOut *rlwe.Ciphertext) {
	ctOut = NewCiphertext(eval.parameters, ctIn.Degree(), ctIn.Level())
	eval.Neg(ctIn, ctOut)
	return
}

// MulScalarThenAdd multiplies ctIn with a scalar adds the result on ctOut.
func (eval *Evaluator) MulScalarThenAdd(ctIn *rlwe.Ciphertext, scalar uint64, ctOut *rlwe.Ciphertext) {
	ringQ := eval.parameters.RingQ().AtLevel(utils.Min(ctIn.Level(), ctOut.Level()))

	// scalar *= (ctOut.PlaintextScale / ctIn.PlaintextScale)
	if ctIn.PlaintextScale.Cmp(ctOut.PlaintextScale) != 0 {
		ringT := eval.parameters.RingT()
		ratio := ring.ModExp(ctIn.PlaintextScale.Uint64(), ringT.SubRings[0].Modulus-2, ringT.SubRings[0].Modulus)
		ratio = ring.BRed(ratio, ctOut.PlaintextScale.Uint64(), ringT.SubRings[0].Modulus, ringT.SubRings[0].BRedConstant)
		scalar = ring.BRed(ratio, scalar, ringT.SubRings[0].Modulus, ringT.SubRings[0].BRedConstant)
	}

	for i := 0; i < ctIn.Degree()+1; i++ {
		ringQ.MulScalarThenAdd(ctIn.Value[i], scalar, ctOut.Value[i])
	}
}

// DropLevel reduces the level of ctIn by levels and returns the result in ctIn.
// No rescaling is applied during this procedure.
func (eval *Evaluator) DropLevel(ctIn *rlwe.Ciphertext, levels int) {
	ctIn.Resize(ctIn.Degree(), ctIn.Level()-levels)
}

// DropLevelNew reduces the level of ctIn by levels and returns the result in a new ctOut.
// No rescaling is applied during this procedure.
func (eval *Evaluator) DropLevelNew(ctIn *rlwe.Ciphertext, levels int) (ctOut *rlwe.Ciphertext) {
	ctOut = ctIn.CopyNew()
	eval.DropLevel(ctOut, levels)
	return
}

// Mul multiplies op0 with op1 without relinearization and returns the result in op2.
// The procedure will panic if either op0 or op1 are have a degree higher than 1.
// The procedure will panic if op2.Degree != op0.Degree + op1.Degree.
func (eval *Evaluator) Mul(op0 *rlwe.Ciphertext, op1 interface{}, op2 *rlwe.Ciphertext) {

	switch op1 := op1.(type) {
	case rlwe.Operand:
		eval.tensorStandard(op0, op1.El(), false, op2)
	case uint64:

		_, level := eval.CheckUnary(op0.El(), op2.El())

		ringQ := eval.parameters.RingQ().AtLevel(level)

		for i := 0; i < op0.Degree()+1; i++ {
			ringQ.MulScalar(op0.Value[i], op1, op2.Value[i])
		}

		op2.MetaData = op0.MetaData
	case []uint64:

		// Retrieves minimum level
		level := utils.Min(op0.Level(), op2.Level())

		// Resizes output to minimum level
		op2.Resize(op0.Degree(), level)

		// Instantiates new plaintext from buffer
		pt := rlwe.NewPlaintextAtLevelFromPoly(level, eval.buffQ[0])
		pt.MetaData = op0.MetaData // Sets the metadata, notably matches scales
		pt.PlaintextScale = rlwe.NewScale(1)

		// Encodes the vector on the plaintext
		if err := eval.Encoder.Encode(op1, pt); err != nil {
			panic(err)
		}

		eval.Mul(op0, pt, op2)
	default:
		panic(fmt.Sprintf("invalid op1.(Type), expected rlwe.Operand, []uint64 or uint64, but got %T", op1))
	}
}

// MulNew multiplies op0 with op1 without relinearization and returns the result in a new op2.
// The procedure will panic if either op0.Degree or op1.Degree > 1.
func (eval *Evaluator) MulNew(op0 *rlwe.Ciphertext, op1 interface{}) (op2 *rlwe.Ciphertext) {

	switch op1 := op1.(type) {
	case rlwe.Operand:
		op2 = NewCiphertext(eval.parameters, op0.Degree()+op1.Degree(), utils.Min(op0.Level(), op1.Level()))
	case uint64, []uint64:
		op2 = NewCiphertext(eval.parameters, op0.Degree(), op0.Level())
	default:
		panic(fmt.Sprintf("invalid op1.(Type), expected rlwe.Operand, []uint64 or uint64, but got %T", op1))
	}

	eval.Mul(op0, op1, op2)

	return
}

// MulRelinNew multiplies op0 with op1 with relinearization and returns the result in a new op2.
// The procedure will panic if either op0.Degree or op1.Degree > 1.
// The procedure will panic if the evaluator was not created with an relinearization key.
func (eval *Evaluator) MulRelinNew(op0 *rlwe.Ciphertext, op1 interface{}) (op2 *rlwe.Ciphertext) {
	switch op1 := op1.(type) {
	case rlwe.Operand:
		op2 = NewCiphertext(eval.parameters, 1, utils.Min(op0.Level(), op1.Level()))
	case uint64, []uint64:
		op2 = NewCiphertext(eval.parameters, 1, op0.Level())
	default:
		panic(fmt.Sprintf("invalid op1.(Type), expected rlwe.Operand, []uint64 or uint64, but got %T", op1))
	}

	eval.MulRelin(op0, op1, op2)

	return
}

// MulRelin multiplies op0 with op1 with relinearization and returns the result in op2.
// The procedure will panic if either op0.Degree or op1.Degree > 1.
// The procedure will panic if op2.Degree != op0.Degree + op1.Degree.
// The procedure will panic if the evaluator was not created with an relinearization key.
func (eval *Evaluator) MulRelin(op0 *rlwe.Ciphertext, op1 interface{}, op2 *rlwe.Ciphertext) {
	switch op1 := op1.(type) {
	case rlwe.Operand:
		eval.tensorStandard(op0, op1.El(), true, op2)
	case uint64, []uint64:
		eval.Mul(op0, op1, op2)
	default:
		panic(fmt.Sprintf("invalid op1.(Type), expected rlwe.Operand, []uint64 or uint64, but got %T", op1))
	}
}

func (eval *Evaluator) tensorStandard(op0 *rlwe.Ciphertext, op1 *rlwe.OperandQ, relin bool, op2 *rlwe.Ciphertext) {

	_, level := eval.CheckBinary(op0.El(), op1.El(), op2.El(), utils.Max(op0.Degree(), op1.Degree()))

	if op2.Level() > level {
		eval.DropLevel(op2, op2.Level()-level)
	}

	if op0.Degree()+op1.Degree() > 2 {
		panic("cannot MulRelin: input elements total degree cannot be larger than 2")
	}

	op2.MetaData = op0.MetaData
	op2.PlaintextScale = op0.PlaintextScale.Mul(op1.PlaintextScale)

	ringQ := eval.parameters.RingQ().AtLevel(level)

	var c00, c01, c0, c1, c2 *ring.Poly

	// Case Ciphertext (x) Ciphertext
	if op0.Degree() == 1 && op1.Degree() == 1 {

		c00 = eval.buffQ[0]
		c01 = eval.buffQ[1]

		c0 = op2.Value[0]
		c1 = op2.Value[1]

		if !relin {
			if op2.Degree() < 2 {
				op2.Resize(2, op2.Level())
			}
			c2 = op2.Value[2]
		} else {
			c2 = eval.buffQ[2]
		}

		// Avoid overwriting if the second input is the output
		var tmp0, tmp1 *rlwe.OperandQ
		if op1.El() == op2.El() {
			tmp0, tmp1 = op1.El(), op0.El()
		} else {
			tmp0, tmp1 = op0.El(), op1.El()
		}

		// Multiply by T * 2^{64} * 2^{64} -> result multipled by T and switched in the Montgomery domain
		ringQ.MulRNSScalarMontgomery(tmp0.Value[0], eval.tMontgomery, c00)
		ringQ.MulRNSScalarMontgomery(tmp0.Value[1], eval.tMontgomery, c01)

		if op0.El() == op1.El() { // squaring case
			ringQ.MulCoeffsMontgomery(c00, tmp1.Value[0], c0) // c0 = c[0]*c[0]
			ringQ.MulCoeffsMontgomery(c01, tmp1.Value[1], c2) // c2 = c[1]*c[1]
			ringQ.MulCoeffsMontgomery(c00, tmp1.Value[1], c1) // c1 = 2*c[0]*c[1]
			ringQ.Add(c1, c1, c1)

		} else { // regular case
			ringQ.MulCoeffsMontgomery(c00, tmp1.Value[0], c0) // c0 = c0[0]*c0[0]
			ringQ.MulCoeffsMontgomery(c01, tmp1.Value[1], c2) // c2 = c0[1]*c1[1]
			ringQ.MulCoeffsMontgomery(c00, tmp1.Value[1], c1)
			ringQ.MulCoeffsMontgomeryThenAdd(c01, tmp1.Value[0], c1) // c1 = c0[0]*c1[1] + c0[1]*c1[0]
		}

		if relin {

			var rlk *rlwe.RelinearizationKey
			var err error
			if rlk, err = eval.CheckAndGetRelinearizationKey(); err != nil {
				panic(fmt.Errorf("cannot relinearize: %w", err))
			}

			tmpCt := &rlwe.Ciphertext{}
			tmpCt.Value = []*ring.Poly{eval.BuffQP[1].Q, eval.BuffQP[2].Q}
			tmpCt.IsNTT = true

			eval.GadgetProduct(level, c2, &rlk.GadgetCiphertext, tmpCt)

			ringQ.Add(op2.Value[0], tmpCt.Value[0], op2.Value[0])
			ringQ.Add(op2.Value[1], tmpCt.Value[1], op2.Value[1])
		}

		// Case Plaintext (x) Ciphertext or Ciphertext (x) Plaintext
	} else {

		if op2.Degree() < op0.Degree() {
			op2.Resize(op0.Degree(), level)
		}

		c00 := eval.buffQ[0]

		// Multiply by T * 2^{64} * 2^{64} -> result multipled by T and switched in the Montgomery domain
		ringQ.MulRNSScalarMontgomery(op1.El().Value[0], eval.tMontgomery, c00)
		for i := range op2.Value {
			ringQ.MulCoeffsMontgomery(op0.Value[i], c00, op2.Value[i])
		}
	}
}

// MulInvariant multiplies op0 by op1 and returns the result in op2.
func (eval *Evaluator) MulInvariant(op0 *rlwe.Ciphertext, op1 interface{}, op2 *rlwe.Ciphertext) {
	switch op1 := op1.(type) {
	case rlwe.Operand:
		switch op1.Degree() {
		case 0:
			eval.tensorStandard(op0, op1.El(), false, op2)
		default:
			eval.tensorInvariant(op0, op1.El(), false, op2)
		}
	case []uint64:

		// Retrieves minimum level
		level := utils.Min(op0.Level(), op2.Level())

		// Resizes output to minimum level
		op2.Resize(op0.Degree(), level)

		// Instantiates new plaintext from buffer
		pt := rlwe.NewPlaintextAtLevelFromPoly(level, eval.buffQ[0])
		pt.MetaData = op0.MetaData // Sets the metadata, notably matches scales
		pt.PlaintextScale = rlwe.NewScale(1)

		// Encodes the vector on the plaintext
		if err := eval.Encoder.Encode(op1, pt); err != nil {
			panic(err)
		}

		eval.MulInvariant(op0, pt, op2)

	case uint64:
		eval.Mul(op0, op1, op2)
	default:
		panic(fmt.Sprintf("invalid op1.(Type), expected rlwe.Operand, []uint64 or uint64, but got %T", op1))
	}
}

// MulInvariantNew multiplies op0 by op1 and returns the result in a newly allocated op2.
// Multiplication is done BFV-style (invariant tensoring).
func (eval *Evaluator) MulInvariantNew(op0 *rlwe.Ciphertext, op1 interface{}) (op2 *rlwe.Ciphertext) {
	switch op1 := op1.(type) {
	case rlwe.Operand:
		op2 = NewCiphertext(eval.parameters, op0.Degree()+op1.Degree(), utils.Min(op0.Level(), op1.Level()))
		eval.MulInvariant(op0, op1, op2)
	case uint64, []uint64:
		op2 = NewCiphertext(eval.parameters, op0.Degree(), op0.Level())
		eval.MulInvariant(op0, op1, op2)
	default:
		panic(fmt.Sprintf("invalid op1.(Type), expected rlwe.Operand, []uint64 or uint64, but got %T", op1))
	}

	return
}

// MulRelinInvariant multiplies op0 by op1 and returns the result in op2.
func (eval *Evaluator) MulRelinInvariant(op0 *rlwe.Ciphertext, op1 interface{}, op2 *rlwe.Ciphertext) {
	switch op1 := op1.(type) {
	case rlwe.Operand:
		switch op1.Degree() {
		case 0:
			eval.tensorStandard(op0, op1.El(), true, op2)
		default:
			eval.tensorInvariant(op0, op1.El(), true, op2)
		}
	case []uint64:

		// Retrieves minimum level
		level := utils.Min(op0.Level(), op2.Level())

		// Resizes output to minimum level
		op2.Resize(op0.Degree(), level)

		// Instantiates new plaintext from buffer
		pt := rlwe.NewPlaintextAtLevelFromPoly(level, eval.buffQ[0])
		pt.MetaData = op0.MetaData // Sets the metadata, notably matches scales
		pt.PlaintextScale = rlwe.NewScale(1)

		// Encodes the vector on the plaintext
		if err := eval.Encoder.Encode(op1, pt); err != nil {
			panic(err)
		}

		eval.MulRelinInvariant(op0, pt, op2)

	case uint64:
		eval.Mul(op0, op1, op2)
	default:
		panic(fmt.Sprintf("invalid op1.(Type), expected rlwe.Operand or uint64, but got %T", op1))
	}
}

// MulRelinInvariantNew multiplies op0 by op1, relinearizes and returns the result in a newly allocated op2.
// Multiplication is done BFV-style (invariant tensoring).
func (eval *Evaluator) MulRelinInvariantNew(op0 *rlwe.Ciphertext, op1 interface{}) (op2 *rlwe.Ciphertext) {
	switch op1 := op1.(type) {
	case rlwe.Operand:
		op2 = NewCiphertext(eval.parameters, 1, utils.Min(op0.Level(), op1.Level()))
		eval.MulRelinInvariant(op0, op1, op2)
	case uint64, []uint64:
		op2 = NewCiphertext(eval.parameters, op0.Degree(), op0.Level())
		eval.MulRelinInvariant(op0, op1, op2)
	default:
		panic(fmt.Sprintf("invalid op1.(Type), expected rlwe.Operand or uint64, but got %T", op1))
	}
	return
}

// tensorInvariant computes (ct0 x ct1) * (t/Q) and stores the result in ctOut.
func (eval *Evaluator) tensorInvariant(ct0 *rlwe.Ciphertext, ct1 *rlwe.OperandQ, relin bool, ctOut *rlwe.Ciphertext) {

	ringQ := eval.parameters.RingQ()

	level := utils.Min(utils.Min(ct0.Level(), ct1.Level()), ctOut.Level())

	levelQMul := eval.levelQMul[level]

	ctOut.Resize(ctOut.Degree(), level)

	// Avoid overwriting if the second input is the output
	var tmp0Q0, tmp1Q0 *rlwe.OperandQ
	if ct1 == ctOut.El() {
		tmp0Q0, tmp1Q0 = ct1, ct0.El()
	} else {
		tmp0Q0, tmp1Q0 = ct0.El(), ct1
	}

	tmp0Q1 := &rlwe.OperandQ{Value: eval.buffQMul[0:3]}
	tmp1Q1 := &rlwe.OperandQ{Value: eval.buffQMul[3:5]}
	tmp2Q1 := tmp0Q1

	eval.modUpAndNTT(level, levelQMul, tmp0Q0, tmp0Q1)

	if tmp0Q0 != tmp1Q0 {
		eval.modUpAndNTT(level, levelQMul, tmp1Q0, tmp1Q1)
	}

	var c2 *ring.Poly
	if !relin {
		if ctOut.Degree() < 2 {
			ctOut.Resize(2, ctOut.Level())
		}
		c2 = ctOut.Value[2]
	} else {
		c2 = eval.buffQ[2]
	}

	tmp2Q0 := &rlwe.OperandQ{Value: []*ring.Poly{ctOut.Value[0], ctOut.Value[1], c2}}

	eval.tensoreLowDeg(level, levelQMul, tmp0Q0, tmp1Q0, tmp2Q0, tmp0Q1, tmp1Q1, tmp2Q1)

	eval.quantize(level, levelQMul, tmp2Q0.Value[0], tmp2Q1.Value[0])
	eval.quantize(level, levelQMul, tmp2Q0.Value[1], tmp2Q1.Value[1])
	eval.quantize(level, levelQMul, tmp2Q0.Value[2], tmp2Q1.Value[2])

	if relin {

		var rlk *rlwe.RelinearizationKey
		var err error
		if eval.EvaluationKeySetInterface != nil {
			if rlk, err = eval.GetRelinearizationKey(); err != nil {
				panic(fmt.Errorf("cannot MulRelin: %w", err))
			}
		} else {
			panic(fmt.Errorf("cannot MulRelin: EvaluationKeySet is nil"))
		}

		tmpCt := &rlwe.Ciphertext{}
		tmpCt.Value = []*ring.Poly{eval.BuffQP[1].Q, eval.BuffQP[2].Q}
		tmpCt.IsNTT = true

		eval.GadgetProduct(level, c2, &rlk.GadgetCiphertext, tmpCt)

		ringQ.Add(ctOut.Value[0], tmpCt.Value[0], ctOut.Value[0])
		ringQ.Add(ctOut.Value[1], tmpCt.Value[1], ctOut.Value[1])
	}

	ctOut.MetaData = ct0.MetaData
	ctOut.PlaintextScale = MulScale(eval.parameters, ct0.PlaintextScale, tmp1Q0.PlaintextScale, ctOut.Level(), true)
}

func (eval *Evaluator) modUpAndNTT(level, levelQMul int, ctQ0, ctQ1 *rlwe.OperandQ) {
	ringQ, ringQMul := eval.parameters.RingQ().AtLevel(level), eval.parameters.RingQMul().AtLevel(levelQMul)
	for i := range ctQ0.Value {
		ringQ.INTT(ctQ0.Value[i], eval.buffQ[0])
		eval.basisExtenderQ1toQ2.ModUpQtoP(level, levelQMul, eval.buffQ[0], ctQ1.Value[i])
		ringQMul.NTTLazy(ctQ1.Value[i], ctQ1.Value[i])
	}
}

func (eval *Evaluator) tensoreLowDeg(level, levelQMul int, ct0Q0, ct1Q0, ct2Q0, ct0Q1, ct1Q1, ct2Q1 *rlwe.OperandQ) {

	ringQ, ringQMul := eval.parameters.RingQ().AtLevel(level), eval.parameters.RingQMul().AtLevel(levelQMul)

	c00 := eval.buffQ[0]
	c01 := eval.buffQ[1]

	ringQ.MForm(ct0Q0.Value[0], c00)
	ringQ.MForm(ct0Q0.Value[1], c01)

	c00M := eval.buffQMul[5]
	c01M := eval.buffQMul[6]

	ringQMul.MForm(ct0Q1.Value[0], c00M)
	ringQMul.MForm(ct0Q1.Value[1], c01M)

	// Squaring case
	if ct0Q0 == ct1Q0 {
		ringQ.MulCoeffsMontgomery(c00, ct0Q0.Value[0], ct2Q0.Value[0]) // c0 = c0[0]*c0[0]
		ringQ.MulCoeffsMontgomery(c01, ct0Q0.Value[1], ct2Q0.Value[2]) // c2 = c0[1]*c0[1]
		ringQ.MulCoeffsMontgomery(c00, ct0Q0.Value[1], ct2Q0.Value[1]) // c1 = 2*c0[0]*c0[1]
		ringQ.AddLazy(ct2Q0.Value[1], ct2Q0.Value[1], ct2Q0.Value[1])

		ringQMul.MulCoeffsMontgomery(c00M, ct0Q1.Value[0], ct2Q1.Value[0])
		ringQMul.MulCoeffsMontgomery(c01M, ct0Q1.Value[1], ct2Q1.Value[2])
		ringQMul.MulCoeffsMontgomery(c00M, ct0Q1.Value[1], ct2Q1.Value[1])
		ringQMul.AddLazy(ct2Q1.Value[1], ct2Q1.Value[1], ct2Q1.Value[1])

		// Normal case
	} else {
		ringQ.MulCoeffsMontgomery(c00, ct1Q0.Value[0], ct2Q0.Value[0]) // c0 = c0[0]*c1[0]
		ringQ.MulCoeffsMontgomery(c01, ct1Q0.Value[1], ct2Q0.Value[2]) // c2 = c0[1]*c1[1]
		ringQ.MulCoeffsMontgomery(c00, ct1Q0.Value[1], ct2Q0.Value[1]) // c1 = c0[0]*c1[1] + c0[1]*c1[0]
		ringQ.MulCoeffsMontgomeryThenAddLazy(c01, ct1Q0.Value[0], ct2Q0.Value[1])

		ringQMul.MulCoeffsMontgomery(c00M, ct1Q1.Value[0], ct2Q1.Value[0])
		ringQMul.MulCoeffsMontgomery(c01M, ct1Q1.Value[1], ct2Q1.Value[2])
		ringQMul.MulCoeffsMontgomery(c00M, ct1Q1.Value[1], ct2Q1.Value[1])
		ringQMul.MulCoeffsMontgomeryThenAddLazy(c01M, ct1Q1.Value[0], ct2Q1.Value[1])
	}
}

func (eval *Evaluator) quantize(level, levelQMul int, c2Q1, c2Q2 *ring.Poly) {

	ringQ, ringQMul := eval.parameters.RingQ().AtLevel(level), eval.parameters.RingQMul().AtLevel(levelQMul)

	// Applies the inverse NTT to the ciphertext, scales down the ciphertext
	// by t/q and reduces its basis from QP to Q

	ringQ.INTTLazy(c2Q1, c2Q1)
	ringQMul.INTTLazy(c2Q2, c2Q2)

	// Extends the basis Q of ct(x) to the basis P and Divides (ct(x)Q -> P) by Q
	eval.basisExtenderQ1toQ2.ModDownQPtoP(level, levelQMul, c2Q1, c2Q2, c2Q2) // QP / Q -> P

	// Centers ct(x)P by (P-1)/2 and extends ct(x)P to the basis Q
	eval.basisExtenderQ1toQ2.ModUpPtoQ(levelQMul, level, c2Q2, c2Q1)

	// (ct(x)/Q)*T, doing so only requires that Q*P > Q*Q, faster but adds error ~|T|
	ringQ.MulScalar(c2Q1, eval.parameters.T(), c2Q1)

	ringQ.NTT(c2Q1, c2Q1)
}

// MulThenAdd multiplies op0 with op1 (without relinearization)^and adds the result on op2.
// The procedure will panic if either op0.Degree() or op1.Degree() > 1.
// The procedure will panic if either op0 == op2 or op1 == op2.
func (eval *Evaluator) MulThenAdd(op0 *rlwe.Ciphertext, op1 interface{}, op2 *rlwe.Ciphertext) {

	switch op1 := op1.(type) {
	case rlwe.Operand:
		eval.mulRelinThenAdd(op0, op1.El(), false, op2)
	case uint64:

		level := utils.Min(op0.Level(), op2.Level())

		ringQ := eval.parameters.RingQ().AtLevel(level)

		// op1 *= (op1.PlaintextScale / op2.PlaintextScale)
		if op0.PlaintextScale.Cmp(op2.PlaintextScale) != 0 {
			s := eval.parameters.RingT().SubRings[0]
			ratio := ring.ModExp(op0.PlaintextScale.Uint64(), s.Modulus-2, s.Modulus)
			ratio = ring.BRed(ratio, op2.PlaintextScale.Uint64(), s.Modulus, s.BRedConstant)
			op1 = ring.BRed(ratio, op1, s.Modulus, s.BRedConstant)
		}

		for i := 0; i < op0.Degree()+1; i++ {
			ringQ.MulScalarThenAdd(op0.Value[i], op1, op2.Value[i])
		}
	case []uint64:

		// Retrieves minimum level
		level := utils.Min(op0.Level(), op2.Level())

		// Resizes output to minimum level
		op2.Resize(op2.Degree(), level)

		// Instantiates new plaintext from buffer
		pt := rlwe.NewPlaintextAtLevelFromPoly(level, eval.buffQ[0])
		pt.MetaData = op0.MetaData // Sets the metadata, notably matches scales

		// op1 *= (op1.PlaintextScale / op2.PlaintextScale)
		if op0.PlaintextScale.Cmp(op2.PlaintextScale) != 0 {
			s := eval.parameters.RingT().SubRings[0]
			ratio := ring.ModExp(op0.PlaintextScale.Uint64(), s.Modulus-2, s.Modulus)
			pt.PlaintextScale = rlwe.NewScale(ring.BRed(ratio, op2.PlaintextScale.Uint64(), s.Modulus, s.BRedConstant))
		} else {
			pt.PlaintextScale = rlwe.NewScale(1)
		}

		// Encodes the vector on the plaintext
		if err := eval.Encoder.Encode(op1, pt); err != nil {
			panic(err)
		}

		eval.MulThenAdd(op0, pt, op2)

	default:
		panic(fmt.Sprintf("invalid op1.(Type), expected rlwe.Operand, []uint64 or uint64, but got %T", op1))

	}
}

// MulRelinThenAdd multiplies op0 with op1 and adds, relinearize the result on op2.
// The procedure will panic if either op0.Degree() or op1.Degree() > 1.
// The procedure will panic if either op0 == op2 or op1 == op2.
func (eval *Evaluator) MulRelinThenAdd(op0, op1 *rlwe.Ciphertext, op2 *rlwe.Ciphertext) {
	eval.mulRelinThenAdd(op0, op1.El(), true, op2)
}

func (eval *Evaluator) mulRelinThenAdd(op0 *rlwe.Ciphertext, op1 *rlwe.OperandQ, relin bool, op2 *rlwe.Ciphertext) {

	_, level := eval.CheckBinary(op0.El(), op1, op2.El(), utils.Max(op0.Degree(), op1.Degree()))

	if op0.El() == op2.El() || op1.El() == op2.El() {
		panic("cannot MulRelinThenAdd: op2 must be different from op0 and op1")
	}

	ringQ := eval.parameters.RingQ().AtLevel(level)
	sT := eval.parameters.RingT().SubRings[0]

	var c00, c01, c0, c1, c2 *ring.Poly

	// Case Ciphertext (x) Ciphertext
	if op0.Degree() == 1 && op1.Degree() == 1 {

		c00 = eval.buffQ[0]
		c01 = eval.buffQ[1]

		c0 = op2.Value[0]
		c1 = op2.Value[1]

		if !relin {
			op2.Resize(2, level)
			c2 = op2.Value[2]
		} else {
			op2.Resize(1, level)
			c2 = eval.buffQ[2]
		}

		tmp0, tmp1 := op0.El(), op1.El()

		// If op0.PlaintextScale * op1.PlaintextScale != op2.PlaintextScale then
		// updates op1.PlaintextScale and op2.PlaintextScale
		var r0 uint64 = 1
		if targetScale := ring.BRed(op0.PlaintextScale.Uint64(), op1.PlaintextScale.Uint64(), sT.Modulus, sT.BRedConstant); op2.PlaintextScale.Cmp(eval.parameters.NewScale(targetScale)) != 0 {
			var r1 uint64
			r0, r1, _ = eval.matchScalesBinary(targetScale, op2.PlaintextScale.Uint64())

			for i := range op2.Value {
				ringQ.MulScalar(op2.Value[i], r1, op2.Value[i])
			}

			op2.PlaintextScale = op2.PlaintextScale.Mul(eval.parameters.NewScale(r1))
		}

		// Multiply by T * 2^{64} * 2^{64} -> result multipled by T and switched in the Montgomery domain
		ringQ.MulRNSScalarMontgomery(tmp0.Value[0], eval.tMontgomery, c00)
		ringQ.MulRNSScalarMontgomery(tmp0.Value[1], eval.tMontgomery, c01)

		// Scales the input to the output scale
		if r0 != 1 {
			ringQ.MulScalar(c00, r0, c00)
			ringQ.MulScalar(c01, r0, c01)
		}

		ringQ.MulCoeffsMontgomeryThenAdd(c00, tmp1.Value[0], c0) // c0 += c[0]*c[0]
		ringQ.MulCoeffsMontgomeryThenAdd(c00, tmp1.Value[1], c1) // c1 += c[0]*c[1]
		ringQ.MulCoeffsMontgomeryThenAdd(c01, tmp1.Value[0], c1) // c1 += c[1]*c[0]

		if relin {

			var rlk *rlwe.RelinearizationKey
			var err error
			if rlk, err = eval.CheckAndGetRelinearizationKey(); err != nil {
				panic(fmt.Errorf("cannot relinearize: %w", err))
			}

			ringQ.MulCoeffsMontgomery(c01, tmp1.Value[1], c2) // c2 += c[1]*c[1]

			tmpCt := &rlwe.Ciphertext{}
			tmpCt.Value = []*ring.Poly{eval.BuffQP[1].Q, eval.BuffQP[2].Q}
			tmpCt.IsNTT = true

			eval.GadgetProduct(level, c2, &rlk.GadgetCiphertext, tmpCt)

			ringQ.Add(op2.Value[0], tmpCt.Value[0], op2.Value[0])
			ringQ.Add(op2.Value[1], tmpCt.Value[1], op2.Value[1])

		} else {
			ringQ.MulCoeffsMontgomeryThenAdd(c01, tmp1.Value[1], c2) // c2 += c[1]*c[1]
		}

		// Case Plaintext (x) Ciphertext or Ciphertext (x) Plaintext
	} else {

		if op2.Degree() < op0.Degree() {
			op2.Resize(op0.Degree(), level)
		}

		c00 := eval.buffQ[0]

		// Multiply by T * 2^{64} * 2^{64} -> result multipled by T and switched in the Montgomery domain
		ringQ.MulRNSScalarMontgomery(op1.El().Value[0], eval.tMontgomery, c00)

		// If op0.PlaintextScale * op1.PlaintextScale != op2.PlaintextScale then
		// updates op1.PlaintextScale and op2.PlaintextScale
		var r0 = uint64(1)
		if targetScale := ring.BRed(op0.PlaintextScale.Uint64(), op1.PlaintextScale.Uint64(), sT.Modulus, sT.BRedConstant); op2.PlaintextScale.Cmp(eval.parameters.NewScale(targetScale)) != 0 {
			var r1 uint64
			r0, r1, _ = eval.matchScalesBinary(targetScale, op2.PlaintextScale.Uint64())

			for i := range op2.Value {
				ringQ.MulScalar(op2.Value[i], r1, op2.Value[i])
			}

			op2.PlaintextScale = op2.PlaintextScale.Mul(eval.parameters.NewScale(r1))
		}

		if r0 != 1 {
			ringQ.MulScalar(c00, r0, c00)
		}

		for i := range op0.Value {
			ringQ.MulCoeffsMontgomeryThenAdd(op0.Value[i], c00, op2.Value[i])
		}
	}
}

// Rescale divides (rounded) ctIn by the last modulus of the moduli chain and returns the result on ctOut.
// This procedure divides the error by the last modulus of the moduli chain while preserving
// the LSB-plaintext bits.
// The procedure will return an error if:
//  1. ctIn.Level() == 0 (the input ciphertext is already at the last modulus)
//  2. ctOut.Level() < ctIn.Level() - 1 (not enough space to store the result)
func (eval *Evaluator) Rescale(ctIn, ctOut *rlwe.Ciphertext) (err error) {

	if ctIn.Level() == 0 {
		return fmt.Errorf("cannot rescale: ctIn already at level 0")
	}

	if ctOut.Level() < ctIn.Level()-1 {
		return fmt.Errorf("cannot rescale: ctOut.Level() < ctIn.Level()-1")
	}

	level := ctIn.Level()
	ringQ := eval.parameters.RingQ().AtLevel(level)

	for i := range ctOut.Value {
		ringQ.DivRoundByLastModulusNTT(ctIn.Value[i], eval.buffQ[0], ctOut.Value[i])
	}

	ctOut.Resize(ctOut.Degree(), level-1)
	ctOut.MetaData = ctIn.MetaData
	ctOut.PlaintextScale = ctIn.PlaintextScale.Div(eval.parameters.NewScale(ringQ.SubRings[level].Modulus))
	return
}

// RelinearizeNew applies the relinearization procedure on ctIn and returns the result in a new ctOut.
func (eval *Evaluator) RelinearizeNew(ctIn *rlwe.Ciphertext) (ctOut *rlwe.Ciphertext) {
	ctOut = NewCiphertext(eval.parameters, 1, ctIn.Level())
	eval.Relinearize(ctIn, ctOut)
	return
}

// ApplyEvaluationKeyNew re-encrypts ctIn under a different key and returns the result in a new ctOut.
// It requires a EvaluationKey, which is computed from the key under which the Ciphertext is currently encrypted,
// and the key under which the Ciphertext will be re-encrypted.
// The procedure will panic if either ctIn.Degree() or ctOut.Degree() != 1.
func (eval *Evaluator) ApplyEvaluationKeyNew(ctIn *rlwe.Ciphertext, evk *rlwe.EvaluationKey) (ctOut *rlwe.Ciphertext) {
	ctOut = NewCiphertext(eval.parameters, ctIn.Degree(), ctIn.Level())
	eval.ApplyEvaluationKey(ctIn, evk, ctOut)
	return
}

// RotateColumnsNew rotates the columns of ctIn by k positions to the left, and returns the result in a newly created element.
// The procedure will panic if the corresponding Galois key has not been generated and attributed to the evaluator.
// The procedure will panic if ctIn.Degree() != 1.
func (eval *Evaluator) RotateColumnsNew(ctIn *rlwe.Ciphertext, k int) (ctOut *rlwe.Ciphertext) {
	ctOut = NewCiphertext(eval.parameters, ctIn.Degree(), ctIn.Level())
	eval.RotateColumns(ctIn, k, ctOut)
	return
}

// RotateColumns rotates the columns of ctIn by k positions to the left and returns the result in ctOut.
// The procedure will panic if the corresponding Galois key has not been generated and attributed to the evaluator.
// The procedure will panic if either ctIn.Degree() or ctOut.Degree() != 1.
func (eval *Evaluator) RotateColumns(ctIn *rlwe.Ciphertext, k int, ctOut *rlwe.Ciphertext) {
	eval.Automorphism(ctIn, eval.parameters.GaloisElement(k), ctOut)
}

// RotateRowsNew swaps the rows of ctIn and returns the result in a new ctOut.
// The procedure will panic if the corresponding Galois key has not been generated and attributed to the evaluator.
// The procedure will panic if ctIn.Degree() != 1.
func (eval *Evaluator) RotateRowsNew(ctIn *rlwe.Ciphertext) (ctOut *rlwe.Ciphertext) {
	ctOut = NewCiphertext(eval.parameters, ctIn.Degree(), ctIn.Level())
	eval.RotateRows(ctIn, ctOut)
	return
}

// RotateRows swaps the rows of ctIn and returns the result in ctOut.
// The procedure will panic if the corresponding Galois key has not been generated and attributed to the evaluator.
// The procedure will panic if either ctIn.Degree() or ctOut.Degree() != 1.
func (eval *Evaluator) RotateRows(ctIn *rlwe.Ciphertext, ctOut *rlwe.Ciphertext) {
	eval.Automorphism(ctIn, eval.parameters.GaloisElementInverse(), ctOut)
}

// RotateHoistedLazyNew applies a series of rotations on the same ciphertext and returns each different rotation in a map indexed by the rotation.
// Results are not rescaled by P.
func (eval *Evaluator) RotateHoistedLazyNew(level int, rotations []int, ctIn *rlwe.Ciphertext, c2DecompQP []ringqp.Poly) (cOut map[int]*rlwe.OperandQP) {
	cOut = make(map[int]*rlwe.OperandQP)
	for _, i := range rotations {
		if i != 0 {
			cOut[i] = rlwe.NewOperandQP(eval.parameters, 1, level, eval.parameters.MaxLevelP())
			eval.AutomorphismHoistedLazy(level, ctIn, c2DecompQP, eval.parameters.GaloisElement(i), cOut[i])
		}
	}

	return
}

// MatchScalesAndLevel updates the both input ciphertexts to ensures that their scale matches.
// To do so it computes t0 * a = ct1 * b such that:
// - ct0.PlaintextScale * a = ct1.PlaintextScale: make the scales match.
// - gcd(a, T) == gcd(b, T) == 1: ensure that the new scale is not a zero divisor if T is not prime.
// - |a+b| is minimal: minimize the added noise by the procedure.
func (eval *Evaluator) MatchScalesAndLevel(ct0, ct1 *rlwe.Ciphertext) {

	r0, r1, _ := eval.matchScalesBinary(ct0.PlaintextScale.Uint64(), ct1.PlaintextScale.Uint64())

	level := utils.Min(ct0.Level(), ct1.Level())

	ringQ := eval.parameters.RingQ().AtLevel(level)

	for _, el := range ct0.Value {
		ringQ.MulScalar(el, r0, el)
	}

	ct0.Resize(ct0.Degree(), level)
	ct0.PlaintextScale = ct0.PlaintextScale.Mul(eval.parameters.NewScale(r0))

	for _, el := range ct1.Value {
		ringQ.MulScalar(el, r1, el)
	}

	ct1.Resize(ct1.Degree(), level)
	ct1.PlaintextScale = ct1.PlaintextScale.Mul(eval.parameters.NewScale(r1))
}

func (eval *Evaluator) matchScalesBinary(scale0, scale1 uint64) (r0, r1, e uint64) {

	ringT := eval.parameters.RingT()

	t := ringT.SubRings[0].Modulus
	tHalf := t >> 1
	BRedConstant := ringT.SubRings[0].BRedConstant

	if utils.GCD(scale0, t) != 1 {
		panic("cannot matchScalesBinary: invalid ciphertext scale: gcd(scale, t) != 1")
	}

	var a = ringT.SubRings[0].Modulus
	var b uint64 = 0
	var A = ring.BRed(ring.ModExp(scale0, t-2, t), scale1, t, BRedConstant)
	var B uint64 = 1

	e = center(A, tHalf, t) + 1

	for A != 0 {
		q := a / A
		a, A = A, a%A
		b, B = B, ring.CRed(t+b-ring.BRed(B, q, t, BRedConstant), t)

		if A != 0 && utils.GCD(A, t) == 1 {
			tmp := center(A, tHalf, t) + center(B, tHalf, t)
			if tmp < e {
				e = tmp
				r0, r1 = A, B
			}
		}
	}

	return
}

func center(x, thalf, t uint64) uint64 {
	if x >= thalf {
		return t - x
	}
	return x
}
