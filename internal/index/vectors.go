// SPDX-FileCopyrightText: 2026 Rillan AI
// SPDX-License-Identifier: Apache-2.0

package index

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
)

func PlaceholderEmbedding(text string) []float32 {
	sum := sha256.Sum256([]byte(text))
	values := make([]float32, 8)
	for i := range values {
		start := i * 4
		value := binary.LittleEndian.Uint32(sum[start : start+4])
		values[i] = float32(value%1000) / 1000
	}
	return values
}

func EncodeEmbedding(values []float32) []byte {
	blob := make([]byte, 4*len(values))
	for i, value := range values {
		binary.LittleEndian.PutUint32(blob[i*4:(i+1)*4], math.Float32bits(value))
	}
	return blob
}

func DecodeEmbedding(blob []byte) ([]float32, error) {
	if len(blob)%4 != 0 {
		return nil, fmt.Errorf("embedding blob length must be divisible by 4")
	}
	values := make([]float32, len(blob)/4)
	for i := range values {
		values[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4 : (i+1)*4]))
	}
	return values, nil
}
