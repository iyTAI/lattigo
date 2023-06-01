package advanced

import (
	"fmt"
	"math"
	"math/big"

	"github.com/tuneinsight/lattigo/v4/ckks"
	"github.com/tuneinsight/lattigo/v4/rlwe"
	"github.com/tuneinsight/lattigo/v4/utils"
	"github.com/tuneinsight/lattigo/v4/utils/bignum"
)

// DFTType is a type used to distinguish different linear transformations.
type DFTType int

// Encode (IDFT) and Decode (DFT) are two available linear transformations for homomorphic encoding and decoding.
const (
	Encode = DFTType(0) // Homomorphic Encoding (IDFT)
	Decode = DFTType(1) // Homomorphic Decoding (DFT)
)

// HomomorphicDFTMatrix is a struct storing the factorized IDFT, DFT matrices, which are
// used to hommorphically encode and decode a ciphertext respectively.
type HomomorphicDFTMatrix struct {
	HomomorphicDFTMatrixLiteral
	Matrices []rlwe.LinearTransform
}

// HomomorphicDFTMatrixLiteral is a struct storing the parameters to generate the factorized DFT/IDFT matrices.
// This struct has mandatory and optional fields.
//
// Mandatory:
//   - DFTType: Encode (a.k.a. CoeffsToSlots) or Decode (a.k.a. SlotsToCoeffs)
//   - LogN: log2(RingDegree)
//   - LogSlots: log2(slots)
//   - LevelStart: starting level of the linear transformation
//   - Levels: depth of the linear transform (i.e. the degree of factorization of the encoding matrix)
//
// Optional:
//   - RepackImag2Real: if true, the imaginary part is repacked into the right n slots of the real part
//   - Scaling: constant by which the matrix is multiplied
//   - BitReversed: if true, then applies the transformation bit-reversed and expects bit-reversed inputs
//   - LogBSGSRatio: log2 of the ratio between the inner and outer loop of the baby-step giant-step algorithm
type HomomorphicDFTMatrixLiteral struct {
	// Mandatory
	Type       DFTType
	LogSlots   int
	LevelStart int
	Levels     []int
	// Optional
	RepackImag2Real bool       // Default: False.
	Scaling         *big.Float // Default 1.0.
	BitReversed     bool       // Default: False.
	LogBSGSRatio    int        // Default: 0.
}

// Depth returns the number of levels allocated to the linear transform.
// If actual == true then returns the number of moduli consumed, else
// returns the factorization depth.
func (d *HomomorphicDFTMatrixLiteral) Depth(actual bool) (depth int) {
	if actual {
		depth = len(d.Levels)
	} else {
		for _, d := range d.Levels {
			depth += d
		}
	}
	return
}

// GaloisElements returns the list of rotations performed during the CoeffsToSlot operation.
func (d *HomomorphicDFTMatrixLiteral) GaloisElements(params ckks.Parameters) (galEls []uint64) {
	rotations := []int{}

	logSlots := d.LogSlots
	logN := params.LogN()
	slots := 1 << logSlots
	dslots := slots
	if logSlots < logN-1 && d.RepackImag2Real {
		dslots <<= 1
		if d.Type == Encode {
			rotations = append(rotations, slots)
		}
	}

	indexCtS := d.computeBootstrappingDFTIndexMap(logN)

	// Coeffs to Slots rotations
	for i, pVec := range indexCtS {
		N1 := rlwe.FindBestBSGSRatio(utils.GetKeys(pVec), dslots, d.LogBSGSRatio)
		rotations = addMatrixRotToList(pVec, rotations, N1, slots, d.Type == Decode && logSlots < logN-1 && i == 0 && d.RepackImag2Real)
	}

	return params.GaloisElements(rotations)
}

// NewHomomorphicDFTMatrixFromLiteral generates the factorized DFT/IDFT matrices for the homomorphic encoding/decoding.
func NewHomomorphicDFTMatrixFromLiteral(d HomomorphicDFTMatrixLiteral, encoder *ckks.Encoder) HomomorphicDFTMatrix {

	params := encoder.Parameters()

	logSlots := d.LogSlots
	logdSlots := logSlots
	if maxLogSlots := params.PlaintextLogDimensions()[1]; logdSlots < maxLogSlots && d.RepackImag2Real {
		logdSlots++
	}

	// CoeffsToSlots vectors
	matrices := []rlwe.LinearTransform{}
	pVecDFT := d.GenMatrices(params.LogN(), params.PlaintextPrecision())

	nbModuliPerRescale := params.PlaintextScaleToModuliRatio()

	level := d.LevelStart
	var idx int
	for i := range d.Levels {

		scale := rlwe.NewScale(params.Q()[level])

		for j := 1; j < nbModuliPerRescale; j++ {
			scale = scale.Mul(rlwe.NewScale(params.Q()[level-j]))
		}

		if d.Levels[i] > 1 {
			y := new(big.Float).SetPrec(scale.Value.Prec()).SetInt64(1)
			y.Quo(y, new(big.Float).SetPrec(scale.Value.Prec()).SetInt64(int64(d.Levels[i])))

			scale.Value = *bignum.Pow(&scale.Value, y)
		}

		for j := 0; j < d.Levels[i]; j++ {

			mat, err := ckks.GenLinearTransform(pVecDFT[idx], encoder, level, scale, logdSlots, d.LogBSGSRatio)

			if err != nil {
				panic(fmt.Errorf("cannot NewHomomorphicDFTMatrixFromLiteral: %w", err))
			}

			matrices = append(matrices, mat)
			idx++
		}

		level -= nbModuliPerRescale
	}

	return HomomorphicDFTMatrix{HomomorphicDFTMatrixLiteral: d, Matrices: matrices}
}

func fftPlainVec(logN, dslots int, roots []*bignum.Complex, pow5 []int) (a, b, c [][]*bignum.Complex) {

	var N, m, index, tt, gap, k, mask, idx1, idx2 int

	N = 1 << logN

	a = make([][]*bignum.Complex, logN)
	b = make([][]*bignum.Complex, logN)
	c = make([][]*bignum.Complex, logN)

	var size int
	if 2*N == dslots {
		size = 2
	} else {
		size = 1
	}

	prec := roots[0].Prec()

	index = 0
	for m = 2; m <= N; m <<= 1 {

		aM := make([]*bignum.Complex, dslots)
		bM := make([]*bignum.Complex, dslots)
		cM := make([]*bignum.Complex, dslots)

		for i := 0; i < dslots; i++ {
			aM[i] = bignum.NewComplex().SetPrec(prec)
			bM[i] = bignum.NewComplex().SetPrec(prec)
			cM[i] = bignum.NewComplex().SetPrec(prec)
		}

		tt = m >> 1

		for i := 0; i < N; i += m {

			gap = N / m
			mask = (m << 2) - 1

			for j := 0; j < m>>1; j++ {

				k = (pow5[j] & mask) * gap

				idx1 = i + j
				idx2 = i + j + tt

				for u := 0; u < size; u++ {
					aM[idx1+u*N].Set(roots[0])
					aM[idx2+u*N].Neg(roots[k])
					bM[idx1+u*N].Set(roots[k])
					cM[idx2+u*N].Set(roots[0])
				}
			}
		}

		a[index] = aM
		b[index] = bM
		c[index] = cM

		index++
	}

	return
}

func ifftPlainVec(logN, dslots int, roots []*bignum.Complex, pow5 []int) (a, b, c [][]*bignum.Complex) {

	var N, m, index, tt, gap, k, mask, idx1, idx2 int

	N = 1 << logN

	a = make([][]*bignum.Complex, logN)
	b = make([][]*bignum.Complex, logN)
	c = make([][]*bignum.Complex, logN)

	var size int
	if 2*N == dslots {
		size = 2
	} else {
		size = 1
	}

	prec := roots[0].Prec()

	index = 0
	for m = N; m >= 2; m >>= 1 {

		aM := make([]*bignum.Complex, dslots)
		bM := make([]*bignum.Complex, dslots)
		cM := make([]*bignum.Complex, dslots)

		for i := 0; i < dslots; i++ {
			aM[i] = bignum.NewComplex().SetPrec(prec)
			bM[i] = bignum.NewComplex().SetPrec(prec)
			cM[i] = bignum.NewComplex().SetPrec(prec)
		}

		tt = m >> 1

		for i := 0; i < N; i += m {

			gap = N / m
			mask = (m << 2) - 1

			for j := 0; j < m>>1; j++ {

				k = ((m << 2) - (pow5[j] & mask)) * gap

				idx1 = i + j
				idx2 = i + j + tt

				for u := 0; u < size; u++ {
					aM[idx1+u*N].Set(roots[0])
					aM[idx2+u*N].Neg(roots[k])
					bM[idx1+u*N].Set(roots[0])
					cM[idx2+u*N].Set(roots[k])
				}
			}
		}

		a[index] = aM
		b[index] = bM
		c[index] = cM

		index++
	}

	return
}

func addMatrixRotToList(pVec map[int]bool, rotations []int, N1, slots int, repack bool) []int {

	if len(pVec) < 3 {
		for j := range pVec {
			if !utils.IsInSlice(j, rotations) {
				rotations = append(rotations, j)
			}
		}
	} else {
		var index int
		for j := range pVec {

			index = (j / N1) * N1

			if repack {
				// Sparse repacking, occurring during the first DFT matrix of the CoeffsToSlots.
				index &= (2*slots - 1)
			} else {
				// Other cases
				index &= (slots - 1)
			}

			if index != 0 && !utils.IsInSlice(index, rotations) {
				rotations = append(rotations, index)
			}

			index = j & (N1 - 1)

			if index != 0 && !utils.IsInSlice(index, rotations) {
				rotations = append(rotations, index)
			}
		}
	}

	return rotations
}

func (d *HomomorphicDFTMatrixLiteral) computeBootstrappingDFTIndexMap(logN int) (rotationMap []map[int]bool) {

	logSlots := d.LogSlots
	ltType := d.Type
	repacki2r := d.RepackImag2Real
	bitreversed := d.BitReversed
	maxDepth := d.Depth(false)

	var level, depth, nextLevel int

	level = logSlots

	rotationMap = make([]map[int]bool, maxDepth)

	// We compute the chain of merge in order or reverse order depending if its DFT or InvDFT because
	// the way the levels are collapsed has an impact on the total number of rotations and keys to be
	// stored. Ex. instead of using 255 + 64 plaintext vectors, we can use 127 + 128 plaintext vectors
	// by reversing the order of the merging.
	merge := make([]int, maxDepth)
	for i := 0; i < maxDepth; i++ {

		depth = int(math.Ceil(float64(level) / float64(maxDepth-i)))

		if ltType == Encode {
			merge[i] = depth
		} else {
			merge[len(merge)-i-1] = depth

		}

		level -= depth
	}

	level = logSlots
	for i := 0; i < maxDepth; i++ {

		if logSlots < logN-1 && ltType == Decode && i == 0 && repacki2r {

			// Special initial matrix for the repacking before Decode
			rotationMap[i] = genWfftRepackIndexMap(logSlots, level)

			// Merges this special initial matrix with the first layer of Decode DFT
			rotationMap[i] = nextLevelfftIndexMap(rotationMap[i], logSlots, 2<<logSlots, level, ltType, bitreversed)

			// Continues the merging with the next layers if the total depth requires it.
			nextLevel = level - 1
			for j := 0; j < merge[i]-1; j++ {
				rotationMap[i] = nextLevelfftIndexMap(rotationMap[i], logSlots, 2<<logSlots, nextLevel, ltType, bitreversed)
				nextLevel--
			}

		} else {

			// First layer of the i-th level of the DFT
			rotationMap[i] = genWfftIndexMap(logSlots, level, ltType, bitreversed)

			// Merges the layer with the next levels of the DFT if the total depth requires it.
			nextLevel = level - 1
			for j := 0; j < merge[i]-1; j++ {
				rotationMap[i] = nextLevelfftIndexMap(rotationMap[i], logSlots, 1<<logSlots, nextLevel, ltType, bitreversed)
				nextLevel--
			}
		}

		level -= merge[i]
	}

	return
}

func genWfftIndexMap(logL, level int, ltType DFTType, bitreversed bool) (vectors map[int]bool) {

	var rot int

	if ltType == Encode && !bitreversed || ltType == Decode && bitreversed {
		rot = 1 << (level - 1)
	} else {
		rot = 1 << (logL - level)
	}

	vectors = make(map[int]bool)
	vectors[0] = true
	vectors[rot] = true
	vectors[(1<<logL)-rot] = true
	return
}

func genWfftRepackIndexMap(logL, level int) (vectors map[int]bool) {
	vectors = make(map[int]bool)
	vectors[0] = true
	vectors[(1 << logL)] = true
	return
}

func nextLevelfftIndexMap(vec map[int]bool, logL, N, nextLevel int, ltType DFTType, bitreversed bool) (newVec map[int]bool) {

	var rot int

	newVec = make(map[int]bool)

	if ltType == Encode && !bitreversed || ltType == Decode && bitreversed {
		rot = (1 << (nextLevel - 1)) & (N - 1)
	} else {
		rot = (1 << (logL - nextLevel)) & (N - 1)
	}

	for i := range vec {
		newVec[i] = true
		newVec[(i+rot)&(N-1)] = true
		newVec[(i-rot)&(N-1)] = true
	}

	return
}

// GenMatrices returns the ordered list of factors of the non-zero diagonales of the IDFT (encoding) or DFT (decoding) matrix.
func (d *HomomorphicDFTMatrixLiteral) GenMatrices(LogN int, prec uint) (plainVector []map[int][]*bignum.Complex) {

	logSlots := d.LogSlots
	slots := 1 << logSlots
	maxDepth := d.Depth(false)
	ltType := d.Type
	bitreversed := d.BitReversed

	logdSlots := logSlots
	if logdSlots < LogN-1 && d.RepackImag2Real {
		logdSlots++
	}

	roots := ckks.GetRootsBigComplex(slots<<2, prec)
	pow5 := make([]int, (slots<<1)+1)
	pow5[0] = 1
	for i := 1; i < (slots<<1)+1; i++ {
		pow5[i] = pow5[i-1] * 5
		pow5[i] &= (slots << 2) - 1
	}

	var fftLevel, depth, nextfftLevel int

	fftLevel = logSlots

	var a, b, c [][]*bignum.Complex
	if ltType == Encode {
		a, b, c = ifftPlainVec(logSlots, 1<<logdSlots, roots, pow5)
	} else {
		a, b, c = fftPlainVec(logSlots, 1<<logdSlots, roots, pow5)
	}

	plainVector = make([]map[int][]*bignum.Complex, maxDepth)

	// We compute the chain of merge in order or reverse order depending if its DFT or InvDFT because
	// the way the levels are collapsed has an impact on the total number of rotations and keys to be
	// stored. Ex. instead of using 255 + 64 plaintext vectors, we can use 127 + 128 plaintext vectors
	// by reversing the order of the merging.
	merge := make([]int, maxDepth)
	for i := 0; i < maxDepth; i++ {

		depth = int(math.Ceil(float64(fftLevel) / float64(maxDepth-i)))

		if ltType == Encode {
			merge[i] = depth
		} else {
			merge[len(merge)-i-1] = depth

		}

		fftLevel -= depth
	}

	fftLevel = logSlots
	for i := 0; i < maxDepth; i++ {

		if logSlots != logdSlots && ltType == Decode && i == 0 && d.RepackImag2Real {

			// Special initial matrix for the repacking before DFT
			plainVector[i] = genRepackMatrix(logSlots, prec, bitreversed)

			// Merges this special initial matrix with the first layer of DFT
			plainVector[i] = multiplyFFTMatrixWithNextFFTLevel(plainVector[i], logSlots, 2*slots, fftLevel, a[logSlots-fftLevel], b[logSlots-fftLevel], c[logSlots-fftLevel], ltType, bitreversed)

			// Continues the merging with the next layers if the total depth requires it.
			nextfftLevel = fftLevel - 1
			for j := 0; j < merge[i]-1; j++ {
				plainVector[i] = multiplyFFTMatrixWithNextFFTLevel(plainVector[i], logSlots, 2*slots, nextfftLevel, a[logSlots-nextfftLevel], b[logSlots-nextfftLevel], c[logSlots-nextfftLevel], ltType, bitreversed)
				nextfftLevel--
			}

		} else {
			// First layer of the i-th level of the DFT
			plainVector[i] = genFFTDiagMatrix(logSlots, fftLevel, a[logSlots-fftLevel], b[logSlots-fftLevel], c[logSlots-fftLevel], ltType, bitreversed)

			// Merges the layer with the next levels of the DFT if the total depth requires it.
			nextfftLevel = fftLevel - 1
			for j := 0; j < merge[i]-1; j++ {
				plainVector[i] = multiplyFFTMatrixWithNextFFTLevel(plainVector[i], logSlots, slots, nextfftLevel, a[logSlots-nextfftLevel], b[logSlots-nextfftLevel], c[logSlots-nextfftLevel], ltType, bitreversed)
				nextfftLevel--
			}
		}

		fftLevel -= merge[i]
	}

	// Repacking after the IDFT (we multiply the last matrix with the vector [1, 1, ..., 1, 1, 0, 0, ..., 0, 0]).
	if logSlots != logdSlots && ltType == Encode && d.RepackImag2Real {
		for j := range plainVector[maxDepth-1] {
			v := plainVector[maxDepth-1][j]
			for x := 0; x < slots; x++ {
				v[x+slots] = bignum.NewComplex().SetPrec(prec)
			}
		}
	}

	scaling := new(big.Float).SetPrec(prec)
	if d.Scaling == nil {
		scaling.SetFloat64(1)
	} else {
		scaling.Set(d.Scaling)
	}

	// If DFT matrix, rescale by 1/N
	if ltType == Encode {
		// Real/Imag extraction 1/2 factor
		if d.RepackImag2Real {
			scaling.Quo(scaling, new(big.Float).SetFloat64(float64(2*slots)))
		} else {
			scaling.Quo(scaling, new(big.Float).SetFloat64(float64(slots)))
		}
	}

	// Spreads the scale accross the matrices
	scaling = bignum.Pow(scaling, new(big.Float).Quo(new(big.Float).SetPrec(prec).SetFloat64(1), new(big.Float).SetPrec(prec).SetFloat64(float64(d.Depth(false)))))

	for j := range plainVector {
		for x := range plainVector[j] {
			v := plainVector[j][x]
			for i := range v {
				v[i][0].Mul(v[i][0], scaling)
				v[i][1].Mul(v[i][1], scaling)
			}
		}
	}

	return
}

func genFFTDiagMatrix(logL, fftLevel int, a, b, c []*bignum.Complex, ltType DFTType, bitreversed bool) (vectors map[int][]*bignum.Complex) {

	var rot int

	if ltType == Encode && !bitreversed || ltType == Decode && bitreversed {
		rot = 1 << (fftLevel - 1)
	} else {
		rot = 1 << (logL - fftLevel)
	}

	vectors = make(map[int][]*bignum.Complex)

	if bitreversed {
		utils.BitReverseInPlaceSlice(a, 1<<logL)
		utils.BitReverseInPlaceSlice(b, 1<<logL)
		utils.BitReverseInPlaceSlice(c, 1<<logL)

		if len(a) > 1<<logL {
			utils.BitReverseInPlaceSlice(a[1<<logL:], 1<<logL)
			utils.BitReverseInPlaceSlice(b[1<<logL:], 1<<logL)
			utils.BitReverseInPlaceSlice(c[1<<logL:], 1<<logL)
		}
	}

	addToDiagMatrix(vectors, 0, a)
	addToDiagMatrix(vectors, rot, b)
	addToDiagMatrix(vectors, (1<<logL)-rot, c)

	return
}

func genRepackMatrix(logL int, prec uint, bitreversed bool) (vectors map[int][]*bignum.Complex) {

	vectors = make(map[int][]*bignum.Complex)

	a := make([]*bignum.Complex, 2<<logL)
	b := make([]*bignum.Complex, 2<<logL)

	for i := 0; i < 1<<logL; i++ {
		a[i] = bignum.NewComplex().SetPrec(prec)
		a[i][0].SetFloat64(1)
		a[i+(1<<logL)] = bignum.NewComplex().SetPrec(prec)
		a[i+(1<<logL)][1].SetFloat64(1)

		b[i] = bignum.NewComplex().SetPrec(prec)
		b[i][1].SetFloat64(1)
		b[i+(1<<logL)] = bignum.NewComplex().SetPrec(prec)
		b[i+(1<<logL)][0].SetFloat64(1)
	}

	addToDiagMatrix(vectors, 0, a)
	addToDiagMatrix(vectors, (1 << logL), b)

	return
}

func multiplyFFTMatrixWithNextFFTLevel(vec map[int][]*bignum.Complex, logL, N, nextLevel int, a, b, c []*bignum.Complex, ltType DFTType, bitreversed bool) (newVec map[int][]*bignum.Complex) {

	var rot int

	newVec = make(map[int][]*bignum.Complex)

	if ltType == Encode && !bitreversed || ltType == Decode && bitreversed {
		rot = (1 << (nextLevel - 1)) & (N - 1)
	} else {
		rot = (1 << (logL - nextLevel)) & (N - 1)
	}

	if bitreversed {
		utils.BitReverseInPlaceSlice(a, 1<<logL)
		utils.BitReverseInPlaceSlice(b, 1<<logL)
		utils.BitReverseInPlaceSlice(c, 1<<logL)

		if len(a) > 1<<logL {
			utils.BitReverseInPlaceSlice(a[1<<logL:], 1<<logL)
			utils.BitReverseInPlaceSlice(b[1<<logL:], 1<<logL)
			utils.BitReverseInPlaceSlice(c[1<<logL:], 1<<logL)
		}
	}

	for i := range vec {
		addToDiagMatrix(newVec, i, rotateAndMulNew(vec[i], 0, a))
		addToDiagMatrix(newVec, (i+rot)&(N-1), rotateAndMulNew(vec[i], rot, b))
		addToDiagMatrix(newVec, (i-rot)&(N-1), rotateAndMulNew(vec[i], -rot, c))
	}

	return
}

func addToDiagMatrix(diagMat map[int][]*bignum.Complex, index int, vec []*bignum.Complex) {
	if diagMat[index] == nil {
		diagMat[index] = make([]*bignum.Complex, len(vec))
		for i := range vec {
			diagMat[index][i] = vec[i].Clone()
		}
	} else {
		add(diagMat[index], vec, diagMat[index])
	}
}

func rotateAndMulNew(a []*bignum.Complex, k int, b []*bignum.Complex) (c []*bignum.Complex) {
	multiplier := bignum.NewComplexMultiplier()

	c = make([]*bignum.Complex, len(a))
	for i := range c {
		c[i] = b[i].Clone()
	}

	mask := int(len(a) - 1)

	for i := 0; i < len(a); i++ {
		multiplier.Mul(c[i], a[(i+k)&mask], c[i])
	}

	return
}

func add(a, b, c []*bignum.Complex) {
	for i := 0; i < len(a); i++ {
		c[i].Add(a[i], b[i])
	}
}
