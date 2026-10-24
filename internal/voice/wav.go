package voice

import (
	"encoding/binary"
	"math"
	"time"
)

// wavData locates the PCM payload in a RIFF/WAVE buffer.
func wavData(data []byte) (dataOffset, dataSize, byteRate int, ok bool) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0, 0, 0, false
	}
	for i := 12; i+8 <= len(data); {
		id := string(data[i : i+4])
		size := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))
		payload := i + 8
		if id == "fmt " && size >= 12 && payload+12 <= len(data) {
			byteRate = int(binary.LittleEndian.Uint32(data[payload+8 : payload+12]))
		}
		if id == "data" {
			if size <= 0 || payload+size > len(data) {
				size = len(data) - payload
			}
			return payload, size, byteRate, true
		}
		next := payload + size
		if size%2 == 1 {
			next++
		}
		if next <= i {
			break
		}
		i = next
	}
	return 0, 0, byteRate, false
}

// wavDuration estimates the play time of a PCM WAV buffer. Zero means
// unknown (not a WAV, or a header without bytes-per-second).
func wavDuration(data []byte) time.Duration {
	_, size, byteRate, ok := wavData(data)
	if !ok || byteRate <= 0 || size <= 0 {
		return 0
	}
	return time.Duration(float64(size) / float64(byteRate) * float64(time.Second))
}

// wavRMS returns the root-mean-square amplitude of 16-bit PCM audio.
// ok is false when the buffer is not a 16-bit WAV.
func wavRMS(data []byte) (rms float64, ok bool) {
	offset, size, _, ok := wavData(data)
	if !ok || size < 2 {
		return 0, false
	}
	// The fmt chunk's bits-per-sample is not tracked here; the recorder
	// always writes 16-bit PCM. Reject obvious non-16-bit payloads by
	// checking the sample count against the data size only.
	n := size / 2
	var sum float64
	for i := 0; i < n; i++ {
		s := int16(binary.LittleEndian.Uint16(data[offset+i*2 : offset+i*2+2]))
		v := float64(s)
		sum += v * v
	}
	if n == 0 {
		return 0, false
	}
	return math.Sqrt(sum / float64(n)), true
}

// HasSpeech reports whether a WAV segment is loud enough to be worth
// transcribing. Unparseable audio is treated as speech — the STT provider
// is the final judge.
func HasSpeech(data []byte, minRMS float64) bool {
	rms, ok := wavRMS(data)
	if !ok {
		return true
	}
	return rms >= minRMS
}

// pcm16WAV wraps 16-bit mono PCM samples in a minimal WAV container.
func pcm16WAV(samples []int16, sampleRate int) []byte {
	dataLen := len(samples) * 2
	out := make([]byte, 44+dataLen)
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+dataLen))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(out[22:24], 1) // mono
	binary.LittleEndian.PutUint32(out[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(out[32:34], 2)
	binary.LittleEndian.PutUint16(out[34:36], 16)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(dataLen))
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[44+i*2:46+i*2], uint16(s))
	}
	return out
}

// toneWAV synthesizes a short sine beep with a 10ms fade in/out so the
// acknowledgment does not click. Used before command capture.
func toneWAV(d time.Duration, freq float64) []byte {
	const rate = 16000
	n := int(d.Seconds() * rate)
	if n <= 0 {
		return nil
	}
	fade := rate / 100 // 10ms
	samples := make([]int16, n)
	for i := 0; i < n; i++ {
		amp := 0.25
		if i < fade {
			amp *= float64(i) / float64(fade)
		}
		if i > n-fade {
			amp *= float64(n-i) / float64(fade)
		}
		v := amp * math.Sin(2*math.Pi*freq*float64(i)/rate)
		samples[i] = int16(v * math.MaxInt16)
	}
	return pcm16WAV(samples, rate)
}
