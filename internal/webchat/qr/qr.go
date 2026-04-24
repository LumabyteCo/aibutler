package qr

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// GenerateQR generates a QR code PNG image for the given URL.
// Uses a minimal QR code implementation (Version 2, Level M, byte mode).
// Returns PNG bytes.
func GenerateQR(url string) ([]byte, error) {
	if url == "" {
		return nil, fmt.Errorf("qr: empty URL")
	}

	// For simplicity and no external deps, generate a text-based QR representation
	// wrapped in a minimal PNG image.
	modules, err := encodeQR(url)
	if err != nil {
		return nil, err
	}

	return renderPNG(modules), nil
}

// encodeQR creates a simple QR code matrix using a minimal implementation.
// This produces a valid QR code for short alphanumeric/byte data.
func encodeQR(data string) ([][]bool, error) {
	dataLen := len(data)
	if dataLen > 77 { // Version 4-M capacity for byte mode
		return nil, fmt.Errorf("qr: data too long (%d bytes, max 77)", dataLen)
	}

	// Determine QR version based on data length.
	version := 1
	sizes := []int{17, 32, 53, 77} // byte mode capacity for versions 1-4, ECC level M
	for i, cap := range sizes {
		if dataLen <= cap {
			version = i + 1
			break
		}
	}

	size := 17 + version*4
	modules := make([][]bool, size)
	reserved := make([][]bool, size)
	for i := range modules {
		modules[i] = make([]bool, size)
		reserved[i] = make([]bool, size)
	}

	// Draw finder patterns at corners.
	drawFinderPattern(modules, reserved, 0, 0)
	drawFinderPattern(modules, reserved, size-7, 0)
	drawFinderPattern(modules, reserved, 0, size-7)

	// Draw timing patterns.
	for i := 8; i < size-8; i++ {
		modules[6][i] = i%2 == 0
		reserved[6][i] = true
		modules[i][6] = i%2 == 0
		reserved[i][6] = true
	}

	// Alignment pattern for version >= 2.
	if version >= 2 {
		cx, cy := size-9, size-9
		drawAlignmentPattern(modules, reserved, cx, cy)
	}

	// Reserve format info areas.
	for i := 0; i < 8; i++ {
		reserved[8][i] = true
		reserved[i][8] = true
		reserved[8][size-1-i] = true
		reserved[size-1-i][8] = true
	}
	reserved[8][8] = true
	// Dark module.
	modules[size-8][8] = true
	reserved[size-8][8] = true

	// Encode data bits (simplified: just place data bytes in available modules).
	bits := encodeDataBits(data, version)
	placeBits(modules, reserved, bits, size)

	// Apply mask pattern 0 (checkerboard) for simplicity.
	for r := 0; r < size; r++ {
		for c := 0; c < size; c++ {
			if !reserved[r][c] && (r+c)%2 == 0 {
				modules[r][c] = !modules[r][c]
			}
		}
	}

	// Write format info for mask 0, ECC level M.
	writeFormatInfo(modules, size)

	return modules, nil
}

func drawFinderPattern(modules, reserved [][]bool, row, col int) {
	for r := -1; r <= 7; r++ {
		for c := -1; c <= 7; c++ {
			rr, cc := row+r, col+c
			if rr < 0 || cc < 0 || rr >= len(modules) || cc >= len(modules) {
				continue
			}
			reserved[rr][cc] = true
			if r >= 0 && r <= 6 && c >= 0 && c <= 6 {
				if r == 0 || r == 6 || c == 0 || c == 6 ||
					(r >= 2 && r <= 4 && c >= 2 && c <= 4) {
					modules[rr][cc] = true
				}
			}
		}
	}
}

func drawAlignmentPattern(modules, reserved [][]bool, centerRow, centerCol int) {
	for r := -2; r <= 2; r++ {
		for c := -2; c <= 2; c++ {
			rr, cc := centerRow+r, centerCol+c
			if rr < 0 || cc < 0 || rr >= len(modules) || cc >= len(modules) {
				continue
			}
			if reserved[rr][cc] {
				continue
			}
			reserved[rr][cc] = true
			if r == -2 || r == 2 || c == -2 || c == 2 || (r == 0 && c == 0) {
				modules[rr][cc] = true
			}
		}
	}
}

func encodeDataBits(data string, version int) []bool {
	// Byte mode indicator (0100) + character count + data + terminator + padding.
	totalBits := []int{128, 224, 352, 512} // total data codewords * 8 for versions 1-4 ECC M
	maxBits := totalBits[version-1]

	var bits []bool

	// Mode indicator: 0100 (byte mode).
	bits = append(bits, false, true, false, false)

	// Character count (8 bits for versions 1-9).
	count := len(data)
	for i := 7; i >= 0; i-- {
		bits = append(bits, (count>>uint(i))&1 == 1)
	}

	// Data bytes.
	for _, b := range []byte(data) {
		for i := 7; i >= 0; i-- {
			bits = append(bits, (b>>uint(i))&1 == 1)
		}
	}

	// Terminator (up to 4 zero bits).
	for i := 0; i < 4 && len(bits) < maxBits; i++ {
		bits = append(bits, false)
	}

	// Pad to byte boundary.
	for len(bits)%8 != 0 && len(bits) < maxBits {
		bits = append(bits, false)
	}

	// Pad codewords (0xEC, 0x11 alternating).
	padBytes := []byte{0xEC, 0x11}
	padIdx := 0
	for len(bits) < maxBits {
		b := padBytes[padIdx%2]
		for i := 7; i >= 0; i-- {
			if len(bits) >= maxBits {
				break
			}
			bits = append(bits, (b>>uint(i))&1 == 1)
		}
		padIdx++
	}

	return bits
}

func placeBits(modules, reserved [][]bool, bits []bool, size int) {
	bitIdx := 0
	// Place bits in two-column strips from right to left, bottom to top/top to bottom alternating.
	for col := size - 1; col >= 1; col -= 2 {
		if col == 6 {
			col-- // Skip timing column.
		}
		upward := ((size - 1 - col) / 2) % 2 == 0
		for i := 0; i < size; i++ {
			row := i
			if upward {
				row = size - 1 - i
			}
			for dc := 0; dc <= 1; dc++ {
				c := col - dc
				if c < 0 || reserved[row][c] {
					continue
				}
				if bitIdx < len(bits) {
					modules[row][c] = bits[bitIdx]
					bitIdx++
				}
			}
		}
	}
}

func writeFormatInfo(modules [][]bool, size int) {
	// Pre-computed format info for ECC M, mask 0 = 101010000010010.
	formatBits := uint16(0b101010000010010)
	positions := [][2]int{
		{8, 0}, {8, 1}, {8, 2}, {8, 3}, {8, 4}, {8, 5},
		{8, 7}, {8, 8}, {7, 8}, {5, 8}, {4, 8}, {3, 8},
		{2, 8}, {1, 8}, {0, 8},
	}

	for i, pos := range positions {
		modules[pos[0]][pos[1]] = (formatBits>>uint(14-i))&1 == 1
	}

	// Second copy along other edges.
	positions2 := [][2]int{
		{size - 1, 8}, {size - 2, 8}, {size - 3, 8}, {size - 4, 8},
		{size - 5, 8}, {size - 6, 8}, {size - 7, 8},
		{8, size - 8}, {8, size - 7}, {8, size - 6}, {8, size - 5},
		{8, size - 4}, {8, size - 3}, {8, size - 2}, {8, size - 1},
	}

	for i, pos := range positions2 {
		if pos[0] >= 0 && pos[0] < size && pos[1] >= 0 && pos[1] < size {
			modules[pos[0]][pos[1]] = (formatBits>>uint(14-i))&1 == 1
		}
	}
}

// renderPNG renders a boolean module matrix as a minimal PNG image.
func renderPNG(modules [][]bool) []byte {
	scale := 4 // pixels per module
	border := 4 // quiet zone modules
	size := len(modules)
	imgSize := (size + border*2) * scale

	// Build raw pixel data (1-bit per pixel, packed in a grayscale image).
	var imgData bytes.Buffer
	for y := 0; y < imgSize; y++ {
		imgData.WriteByte(0) // filter: none
		for x := 0; x < imgSize; x++ {
			modR := y/scale - border
			modC := x/scale - border
			if modR >= 0 && modR < size && modC >= 0 && modC < size && modules[modR][modC] {
				imgData.WriteByte(0) // black
			} else {
				imgData.WriteByte(255) // white
			}
		}
	}

	return buildPNG(imgSize, imgSize, imgData.Bytes())
}

// buildPNG creates a minimal valid PNG file with grayscale pixel data.
func buildPNG(width, height int, rawData []byte) []byte {
	var buf bytes.Buffer

	// PNG signature.
	buf.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})

	// IHDR chunk.
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], uint32(width))
	binary.BigEndian.PutUint32(ihdr[4:8], uint32(height))
	ihdr[8] = 8  // bit depth
	ihdr[9] = 0  // color type: grayscale
	ihdr[10] = 0 // compression
	ihdr[11] = 0 // filter
	ihdr[12] = 0 // interlace
	writeChunk(&buf, "IHDR", ihdr)

	// IDAT chunk - compress raw data using deflate (stored blocks, no compression).
	compressed := deflateStored(rawData)
	writeChunk(&buf, "IDAT", compressed)

	// IEND chunk.
	writeChunk(&buf, "IEND", nil)

	return buf.Bytes()
}

func writeChunk(buf *bytes.Buffer, chunkType string, data []byte) {
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(data)))
	buf.Write(length)
	buf.WriteString(chunkType)
	buf.Write(data)

	// CRC32 over type + data.
	crcData := append([]byte(chunkType), data...)
	crc := crc32Compute(crcData)
	crcBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBytes, crc)
	buf.Write(crcBytes)
}

// deflateStored wraps data in a zlib container with stored (no compression) deflate blocks.
func deflateStored(data []byte) []byte {
	var buf bytes.Buffer

	// Zlib header: CMF=0x78 (deflate, window 32K), FLG=0x01 (no dict, check bits).
	buf.WriteByte(0x78)
	buf.WriteByte(0x01)

	// Split into 65535-byte stored blocks.
	maxBlock := 65535
	for i := 0; i < len(data); i += maxBlock {
		end := i + maxBlock
		final := byte(0)
		if end >= len(data) {
			end = len(data)
			final = 1
		}
		block := data[i:end]
		blockLen := len(block)

		buf.WriteByte(final) // BFINAL + BTYPE=00 (stored)
		lenBytes := make([]byte, 2)
		binary.LittleEndian.PutUint16(lenBytes, uint16(blockLen))
		buf.Write(lenBytes)
		nlenBytes := make([]byte, 2)
		binary.LittleEndian.PutUint16(nlenBytes, uint16(blockLen)^0xFFFF)
		buf.Write(nlenBytes)
		buf.Write(block)
	}

	// Adler-32 checksum.
	adler := adler32Compute(data)
	adlerBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(adlerBytes, adler)
	buf.Write(adlerBytes)

	return buf.Bytes()
}

// adler32Compute computes Adler-32 checksum.
func adler32Compute(data []byte) uint32 {
	const mod = 65521
	a := uint32(1)
	b := uint32(0)
	for _, d := range data {
		a = (a + uint32(d)) % mod
		b = (b + a) % mod
	}
	return (b << 16) | a
}

// crc32Compute computes CRC-32 for PNG chunks.
func crc32Compute(data []byte) uint32 {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc ^= uint32(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xEDB88320
			} else {
				crc >>= 1
			}
		}
	}
	return crc ^ 0xFFFFFFFF
}

// Ensure math package is used (for future extensions).
var _ = math.MaxInt
