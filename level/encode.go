package level

import (
	"errors"
	"fmt"
	"strconv"
)

// Encode validates terrain, placements, and zones and returns UTF-8 JSON in a
// stable field order. It writes version 1 when there are no zones, so a level
// that uses no new feature stays readable by every tool, and version 2
// otherwise. It preserves the supplied row, placement, and zone order, does not
// modify any slice, and returns no bytes on validation failure. The caller owns
// the returned bytes.
func Encode(rows []string, placements []Placement, zones []Zone) ([]byte, error) {
	if len(rows) == 0 || len(rows) > maxSide {
		return nil, fmt.Errorf("board must have 1–%d rows", maxSide)
	}
	if len(placements) > maxPlacements {
		return nil, fmt.Errorf("at most %d placements are allowed", maxPlacements)
	}
	if len(zones) > maxZones {
		return nil, fmt.Errorf("at most %d zones are allowed", maxZones)
	}
	width := len(rows[0])
	for y, row := range rows {
		if err := checkRow(row, y, width); err != nil {
			return nil, err
		}
	}
	b := Board{rows: rows, placements: placements, zones: zones}
	if err := b.checkPlacements(); err != nil {
		return nil, err
	}
	if err := b.checkZones(); err != nil {
		return nil, err
	}

	// Validation limits rows, kinds, and roles to ASCII that needs no JSON
	// escaping. Reserve the maximum per entry so emission never grows the buffer.
	data := make([]byte, 0, 64+len(rows)*(width+8)+len(placements)*96+len(zones)*160)
	version := byte('1')
	if len(zones) != 0 {
		version = '2'
	}
	data = append(data, "{\n  \"version\": "...)
	data = append(data, version)
	data = append(data, ",\n  \"rows\": ["...)
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
	data = append(data, ']')
	if len(zones) != 0 {
		data = append(data, ",\n  \"zones\": ["...)
		for i, z := range zones {
			if i != 0 {
				data = append(data, ',')
			}
			data = append(data, "\n    {\"id\": "...)
			data = strconv.AppendUint(data, uint64(z.ID), 10)
			data = append(data, ", \"kind\": \""...)
			data = append(data, z.Kind...)
			data = append(data, "\", \"role\": \""...)
			data = append(data, z.Role...)
			data = append(data, '"')
			for _, field := range []struct {
				name  string
				value int
			}{{"x", z.X}, {"y", z.Y}, {"w", z.W}, {"h", z.H}, {"bottom", z.Bottom}, {"top", z.Top}} {
				data = append(data, ", \""...)
				data = append(data, field.name...)
				data = append(data, "\": "...)
				data = strconv.AppendInt(data, int64(field.value), 10)
			}
			data = append(data, '}')
		}
		data = append(data, "\n  ]"...)
	}
	data = append(data, "\n}\n"...)
	if len(data) > MaxBytes {
		return nil, errors.New("encoded level exceeds the byte limit")
	}
	return data, nil
}
