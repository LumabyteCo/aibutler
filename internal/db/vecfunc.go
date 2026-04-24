package db

import (
	"encoding/binary"
	"math"

	"github.com/ncruces/go-sqlite3"
)

// registerVecFunctions registers pure Go vector distance SQL functions on a connection.
// These run natively in Go (not WASM), so they work with any ncruces version.
func registerVecFunctions(conn *sqlite3.Conn) error {
	// vec_distance_cosine(a BLOB, b BLOB) → REAL
	// Returns cosine distance (1 - cosine_similarity). Range: 0 (identical) to 2 (opposite).
	if err := conn.CreateFunction("vec_distance_cosine", 2,
		sqlite3.DETERMINISTIC|sqlite3.INNOCUOUS, vecDistanceCosine); err != nil {
		return err
	}

	// vec_distance_l2(a BLOB, b BLOB) → REAL
	// Returns Euclidean (L2) distance. Range: 0 (identical) to +inf.
	if err := conn.CreateFunction("vec_distance_l2", 2,
		sqlite3.DETERMINISTIC|sqlite3.INNOCUOUS, vecDistanceL2); err != nil {
		return err
	}

	return nil
}

// vecDistanceCosine computes cosine distance between two float32 vectors stored as BLOBs.
func vecDistanceCosine(ctx sqlite3.Context, args ...sqlite3.Value) {
	a := blobToFloat32s(args[0].RawBlob())
	b := blobToFloat32s(args[1].RawBlob())
	if len(a) != len(b) || len(a) == 0 {
		ctx.ResultFloat(1.0) // neutral distance on dimension mismatch
		return
	}

	var dot, normA, normB float64
	for i := range a {
		fa, fb := float64(a[i]), float64(b[i])
		dot += fa * fb
		normA += fa * fa
		normB += fb * fb
	}

	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		ctx.ResultFloat(1.0)
		return
	}

	ctx.ResultFloat(1.0 - dot/denom)
}

// vecDistanceL2 computes Euclidean distance between two float32 vectors stored as BLOBs.
func vecDistanceL2(ctx sqlite3.Context, args ...sqlite3.Value) {
	a := blobToFloat32s(args[0].RawBlob())
	b := blobToFloat32s(args[1].RawBlob())
	if len(a) != len(b) || len(a) == 0 {
		ctx.ResultFloat(math.MaxFloat64)
		return
	}

	var sum float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sum += d * d
	}

	ctx.ResultFloat(math.Sqrt(sum))
}

// blobToFloat32s decodes a little-endian byte slice into a float32 slice.
func blobToFloat32s(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	n := len(b) / 4
	result := make([]float32, n)
	for i := range n {
		result[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return result
}
