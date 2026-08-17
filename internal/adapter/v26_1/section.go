package v26_1

import (
	"errors"
	"fmt"

	"github.com/go-theft-craft/headless-minecraft/world"
)

// This file decodes protocol 775's chunk column: the paletted containers that
// carry a section's block states.
//
// It is written from the 26.1.2 server's own serializer rather than from
// memory, and checked against a column captured from a real Paper 26.1 server.
// Both mattered. The layout is not the one a reader of earlier versions would
// write down — a section carries two counts, not one, and the long array has
// no length prefix — and either mistake decodes to plausible wrong blocks
// rather than to an error.
//
// From LevelChunkSection.write and PalettedContainer.Data.write:
//
//	section   := nonEmptyBlockCount:short fluidCount:short states biomes
//	container := bitsInStorage:byte palette longs:long[fixed]
//
// The palette depends on the bit width, and the widths differ per container
// because the two use different strategies (Strategy.createForBlockStates and
// createForBiomes):
//
//	0                    one varint: every entry is that value, no longs
//	1..8 states          a varint count and that many varints
//	1..3 biomes          the same, at the narrower biome widths
//	anything wider       the global palette: no palette on the wire at all
//
// The long array carries no count of its own — vanilla writes it with
// writeFixedSizeLongArray — so its length is derived, and getting it wrong
// silently misaligns every section that follows. That is what the exact-fit
// check at the end of splitColumn775 exists to catch.
const (
	blocksPerSection775 = 4096
	biomesPerSection775 = 64
	// The widest bit widths that still carry a palette. Wider means the global
	// palette, whose entries are the registry's own IDs.
	maxBlockPaletteBits775 = 8
	maxBiomePaletteBits775 = 3
	// sectionsPerColumnLimit bounds a column before the section count is
	// known, so a malformed blob cannot be read as millions of sections. No
	// vanilla dimension is close: the overworld is 24.
	sectionsPerColumnLimit = 128
)

// dimensionTypeRegistry is the registry whose entries carry a dimension's
// minimum build height.
const dimensionTypeRegistry = "minecraft:dimension_type"

// ErrColumnNotDecodable reports a chunk column this client could not read.
var ErrColumnNotDecodable = errors.New("chunk column is not decodable")

// splitColumn775 slices a column's blob into one byte range per section's
// block states, keyed by the section's index in the world.
//
// bottom is the index of the column's lowest section, which is the dimension's
// minimum build height divided by sixteen. The blob does not carry it: it is a
// property of the dimension, and the section that a reader thinks is at y=0 is
// wrong by four in the overworld if it assumes otherwise.
//
// The biome container of each section is walked and dropped. The world keeps
// no biome state, and walking it is not optional: it sits between one
// section's blocks and the next one's.
func splitColumn775(data []byte, bottom int) ([]world.SectionData, error) {
	r := &columnReader{data: data}

	var sections []world.SectionData
	for index := 0; r.pos < len(data); index++ {
		if index >= sectionsPerColumnLimit {
			return nil, fmt.Errorf("%w: more than %d sections", ErrColumnNotDecodable, sectionsPerColumnLimit)
		}
		// nonEmptyBlockCount and fluidCount. Neither is kept: the first counts
		// blocks that are not any of the three air states, which is a block
		// semantic this package does not own, and the second is only useful to
		// a client that ticks fluids.
		if _, err := r.short(); err != nil {
			return nil, err
		}
		if _, err := r.short(); err != nil {
			return nil, err
		}

		states, err := r.container(blocksPerSection775, maxBlockPaletteBits775)
		if err != nil {
			return nil, err
		}
		if _, err := r.container(biomesPerSection775, maxBiomePaletteBits775); err != nil {
			return nil, err
		}

		sections = append(sections, world.SectionData{
			Y:      bottom + index,
			Raw:    states,
			Decode: decodeSection775,
		})
	}

	// The loop stops when the blob runs out, so it always ends on a boundary
	// it believes in. What proves the belief is that the last section ended
	// exactly at the end: a misread bit width or a missed field leaves the
	// cursor short or past, and this is the only signal that says so before
	// the wrong blocks are handed to a consumer.
	if r.pos != len(data) {
		return nil, fmt.Errorf(
			"%w: read %d of %d bytes", ErrColumnNotDecodable, r.pos, len(data),
		)
	}

	return sections, nil
}

// decodeSection775 turns one section's block-state container into 4096 states.
//
// It is pure, as the world requires: two readers racing to decode the same
// bytes compute the same answer.
func decodeSection775(raw []byte) ([]uint32, error) {
	r := &columnReader{data: raw}

	bits, err := r.byteValue()
	if err != nil {
		return nil, world.ErrSectionNotDecodable
	}

	palette, err := readPalette(r, int(bits), maxBlockPaletteBits775)
	if err != nil {
		return nil, world.ErrSectionNotDecodable
	}

	states := make([]uint32, blocksPerSection775)
	// A single-valued container has no long array at all, because there is
	// nothing to distinguish: every one of the 4096 entries is that value.
	if bits == 0 {
		for i := range states {
			states[i] = palette[0]
		}

		return states, nil
	}

	perLong := 64 / int(bits)
	packed, err := r.take(longsFor(blocksPerSection775, perLong) * 8)
	if err != nil {
		return nil, world.ErrSectionNotDecodable
	}

	mask := uint64(1)<<bits - 1
	for i := range states {
		cell := i / perLong
		word := beUint64(packed[cell*8:])
		value := uint32(word >> ((i - cell*perLong) * int(bits)) & mask)
		// An indirect palette maps the stored value to a state ID; the global
		// palette stores the ID itself.
		if palette != nil {
			if int(value) >= len(palette) {
				return nil, world.ErrSectionNotDecodable
			}
			value = palette[value]
		}
		states[i] = value
	}

	return states, nil
}

// longsFor is the length vanilla's SimpleBitStorage allocates: entries are
// packed whole into each long and never straddle one, so the remainder costs a
// whole long.
func longsFor(entries, perLong int) int { return (entries + perLong - 1) / perLong }

func beUint64(b []byte) uint64 {
	return uint64(b[7]) | uint64(b[6])<<8 | uint64(b[5])<<16 | uint64(b[4])<<24 |
		uint64(b[3])<<32 | uint64(b[2])<<40 | uint64(b[1])<<48 | uint64(b[0])<<56
}

// readPalette reads the palette that follows a bit width, or returns nil for
// the global palette, which writes nothing.
func readPalette(r *columnReader, bits, maxPaletteBits int) ([]uint32, error) {
	switch {
	case bits == 0:
		value, err := r.varint()
		if err != nil {
			return nil, err
		}

		return []uint32{uint32(value)}, nil

	case bits <= maxPaletteBits:
		count, err := r.varint()
		if err != nil {
			return nil, err
		}
		if count < 0 || count > 1<<bits {
			return nil, fmt.Errorf("%w: palette of %d at %d bits", ErrColumnNotDecodable, count, bits)
		}
		palette := make([]uint32, count)
		for i := range palette {
			value, err := r.varint()
			if err != nil {
				return nil, err
			}
			palette[i] = uint32(value)
		}

		return palette, nil

	default:
		return nil, nil
	}
}

// columnReader walks a column's bytes. It is a cursor rather than a decoder:
// the shared protocol module owns wire types, and this reads the one structure
// it models as an opaque byte array.
type columnReader struct {
	data []byte
	pos  int
}

func (r *columnReader) byteValue() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, fmt.Errorf("%w: byte past the end", ErrColumnNotDecodable)
	}
	value := r.data[r.pos]
	r.pos++

	return value, nil
}

func (r *columnReader) short() (int16, error) {
	if r.pos+2 > len(r.data) {
		return 0, fmt.Errorf("%w: short past the end", ErrColumnNotDecodable)
	}
	value := int16(r.data[r.pos])<<8 | int16(r.data[r.pos+1])
	r.pos += 2

	return value, nil
}

func (r *columnReader) varint() (int32, error) {
	var value uint32
	for shift := 0; ; shift += 7 {
		if shift >= 35 {
			return 0, fmt.Errorf("%w: varint longer than five bytes", ErrColumnNotDecodable)
		}
		b, err := r.byteValue()
		if err != nil {
			return 0, err
		}
		value |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			break
		}
	}

	return int32(value), nil
}

func (r *columnReader) take(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.data) {
		return nil, fmt.Errorf("%w: %d bytes past the end", ErrColumnNotDecodable, n)
	}
	taken := r.data[r.pos : r.pos+n]
	r.pos += n

	return taken, nil
}

// container walks one paletted container and returns its whole encoding,
// header included, so decodeSection775 can read it again without the split
// having to keep the palette alongside the bytes.
func (r *columnReader) container(entries, maxPaletteBits int) ([]byte, error) {
	start := r.pos

	bits, err := r.byteValue()
	if err != nil {
		return nil, err
	}
	if _, err := readPalette(r, int(bits), maxPaletteBits); err != nil {
		return nil, err
	}
	if bits > 0 {
		if _, err := r.take(longsFor(entries, 64/int(bits)) * 8); err != nil {
			return nil, err
		}
	}

	return r.data[start:r.pos], nil
}

// columnSections turns a column's blob into the sections the world stores.
//
// A column that cannot be placed or cannot be read is kept whole and
// undecoded, exactly as this adapter kept every column before the decoder
// existed: the bytes are not lost, and a block lookup in the column reports
// that it cannot be decoded rather than reporting air. Air is the answer a
// consumer walks into.
func columnSections(floor *columnFloor, data []byte) []world.SectionData {
	whole := []world.SectionData{{Y: 0, Raw: data}}
	if !floor.placed {
		return whole
	}
	sections, err := splitColumn775(data, floor.section)
	if err != nil {
		return whole
	}

	return sections
}
