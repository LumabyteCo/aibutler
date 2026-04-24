package db_test

import (
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/LumabyteCo/aibutler/internal/db"
)

// float32sToBlob converts a float32 slice to a little-endian BLOB for SQL binding.
func float32sToBlob(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(db.Config{Path: ":memory:"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestVecDistanceCosineIdentical(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	a := float32sToBlob([]float32{1, 0, 0})
	var dist float64
	err := d.Conn().QueryRowContext(ctx,
		"SELECT vec_distance_cosine(?, ?)", a, a).Scan(&dist)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if math.Abs(dist) > 1e-6 {
		t.Errorf("identical vectors: distance = %f, want ~0", dist)
	}
}

func TestVecDistanceCosineOrthogonal(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	a := float32sToBlob([]float32{1, 0, 0})
	b := float32sToBlob([]float32{0, 1, 0})
	var dist float64
	err := d.Conn().QueryRowContext(ctx,
		"SELECT vec_distance_cosine(?, ?)", a, b).Scan(&dist)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if math.Abs(dist-1.0) > 1e-6 {
		t.Errorf("orthogonal vectors: distance = %f, want ~1.0", dist)
	}
}

func TestVecDistanceCosineOpposite(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	a := float32sToBlob([]float32{1, 0, 0})
	b := float32sToBlob([]float32{-1, 0, 0})
	var dist float64
	err := d.Conn().QueryRowContext(ctx,
		"SELECT vec_distance_cosine(?, ?)", a, b).Scan(&dist)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if math.Abs(dist-2.0) > 1e-6 {
		t.Errorf("opposite vectors: distance = %f, want ~2.0", dist)
	}
}

func TestVecDistanceCosineSimilar(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	a := float32sToBlob([]float32{1, 1, 0})
	b := float32sToBlob([]float32{1, 0, 0})
	var dist float64
	err := d.Conn().QueryRowContext(ctx,
		"SELECT vec_distance_cosine(?, ?)", a, b).Scan(&dist)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	// Cosine similarity of [1,1,0] and [1,0,0] = 1/√2 ≈ 0.707
	// Cosine distance = 1 - 0.707 ≈ 0.293
	if dist < 0.2 || dist > 0.4 {
		t.Errorf("similar vectors: distance = %f, want ~0.293", dist)
	}
}

func TestVecDistanceCosineDimensionMismatch(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	a := float32sToBlob([]float32{1, 0, 0})
	b := float32sToBlob([]float32{1, 0})
	var dist float64
	err := d.Conn().QueryRowContext(ctx,
		"SELECT vec_distance_cosine(?, ?)", a, b).Scan(&dist)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if dist != 1.0 {
		t.Errorf("dimension mismatch: distance = %f, want 1.0", dist)
	}
}

func TestVecDistanceL2Identical(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	a := float32sToBlob([]float32{3, 4, 0})
	var dist float64
	err := d.Conn().QueryRowContext(ctx,
		"SELECT vec_distance_l2(?, ?)", a, a).Scan(&dist)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if math.Abs(dist) > 1e-6 {
		t.Errorf("identical vectors: L2 distance = %f, want ~0", dist)
	}
}

func TestVecDistanceL2KnownValue(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	a := float32sToBlob([]float32{0, 0, 0})
	b := float32sToBlob([]float32{3, 4, 0})
	var dist float64
	err := d.Conn().QueryRowContext(ctx,
		"SELECT vec_distance_l2(?, ?)", a, b).Scan(&dist)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if math.Abs(dist-5.0) > 1e-6 {
		t.Errorf("L2 distance = %f, want 5.0", dist)
	}
}

func TestVecDistanceCosineInSQL(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	conn := d.Conn()

	// Create a test table with embeddings.
	conn.ExecContext(ctx, `CREATE TABLE test_vecs (id INTEGER PRIMARY KEY, label TEXT, embedding BLOB)`)
	conn.ExecContext(ctx, `INSERT INTO test_vecs (label, embedding) VALUES (?, ?)`,
		"cat", float32sToBlob([]float32{0.9, 0.1, 0.0}))
	conn.ExecContext(ctx, `INSERT INTO test_vecs (label, embedding) VALUES (?, ?)`,
		"dog", float32sToBlob([]float32{0.8, 0.2, 0.0}))
	conn.ExecContext(ctx, `INSERT INTO test_vecs (label, embedding) VALUES (?, ?)`,
		"car", float32sToBlob([]float32{0.0, 0.1, 0.9}))

	// Search for nearest to "cat-like" query.
	query := float32sToBlob([]float32{1.0, 0.0, 0.0})
	rows, err := conn.QueryContext(ctx,
		`SELECT label, vec_distance_cosine(embedding, ?) as dist
		 FROM test_vecs ORDER BY dist ASC LIMIT 2`, query)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var labels []string
	for rows.Next() {
		var label string
		var dist float64
		if err := rows.Scan(&label, &dist); err != nil {
			t.Fatalf("scan: %v", err)
		}
		labels = append(labels, label)
	}

	if len(labels) != 2 {
		t.Fatalf("got %d results, want 2", len(labels))
	}
	if labels[0] != "cat" {
		t.Errorf("nearest = %q, want cat", labels[0])
	}
	if labels[1] != "dog" {
		t.Errorf("second nearest = %q, want dog", labels[1])
	}
}

func TestVecDistanceZeroVector(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	a := float32sToBlob([]float32{0, 0, 0})
	b := float32sToBlob([]float32{1, 0, 0})
	var dist float64
	err := d.Conn().QueryRowContext(ctx,
		"SELECT vec_distance_cosine(?, ?)", a, b).Scan(&dist)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if dist != 1.0 {
		t.Errorf("zero vector: distance = %f, want 1.0", dist)
	}
}

func TestVecDistanceEmptyBlob(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	a := []byte{}
	b := float32sToBlob([]float32{1, 0, 0})
	var dist float64
	err := d.Conn().QueryRowContext(ctx,
		"SELECT vec_distance_cosine(?, ?)", a, b).Scan(&dist)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if dist != 1.0 {
		t.Errorf("empty blob: distance = %f, want 1.0", dist)
	}
}
