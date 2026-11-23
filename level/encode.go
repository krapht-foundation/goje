package level

import (
	"errors"
	"fmt"
	"strconv"
)

// Encode validates v1 terrain and placements and returns UTF-8 JSON in a
// stable field order. It preserves the supplied row and placement order,
// does not modify either slice, and returns no bytes on validation failure.
// The caller owns the returned bytes.
func Encode(rows []string, placements []Placement) ([]byte, error) {
	if len(rows) == 0 || len(rows) > maxSide {
		return nil, fmt.Errorf("board must have 1–%d rows", maxSide)
	}
	if len(placements) > maxPlacements {
		return nil, fmt.Errorf("at most %d placements are allowed", maxPlacements)
	}
	width := len(rows[0])
	for y, row := range rows {
		if err := checkRow(row, y, width); err != nil {
			return nil, err
		}
	}
	if err := (Board{rows: rows, placements: placements}).checkPlacements(); err != nil {
		return nil, err
	}

	// Validation limits rows and kinds to ASCII that needs no JSON escaping.
	// Reserve the maximum per placement so emission never grows the buffer.
	data := make([]byte, 0, 64+len(rows)*(width+8)+len(placements)*96)
	data = append(data, "{\n  \"version\": 1,\n  \"rows\": ["...)
	for i, row := range rows {
		if i != 0 {
			data = append(data, ',')
		}
		data = append(data, "\n    \""...)
		data = append(data, row...)
		data = append(data, '"')
	}
	data = append(data, "\n  ],\n  \"placements\": ["...)
	for i, p := range placements {
		if i != 0 {
			data = append(data, ',')
		}
		data = append(data, "\n    {\"id\": "...)
		data = strconv.AppendUint(data, uint64(p.ID), 10)
		data = append(data, ", \"kind\": \""...)
		data = append(data, p.Kind...)
		data = append(data, "\", \"x\": "...)
		data = strconv.AppendInt(data, int64(p.X), 10)
		data = append(data, ", \"y\": "...)
		data = strconv.AppendInt(data, int64(p.Y), 10)
		data = append(data, '}')
	}
	if len(placements) != 0 {
		data = append(data, "\n  "...)
	}
	data = append(data, "]\n}\n"...)
	if len(data) > MaxBytes {
		return nil, errors.New("encoded level exceeds the v1 byte limit")
	}
	return data, nil
}
