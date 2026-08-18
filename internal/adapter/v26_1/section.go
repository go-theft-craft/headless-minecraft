package v26_1

import (
	"fmt"

	"github.com/go-theft-craft/minecraft-protocol/wire/java/chunk"

	"github.com/go-theft-craft/headless-minecraft/world"
)

// This file places protocol 775's chunk column in the world.
//
// Reading the column is not this package's work any more. The paletted
// containers a section is packed into are the same bytes for anything that
// speaks 775, so the layout lives in minecraft-protocol's wire/java/chunk,
// which the generated codecs stop short of because what is inside the blob
// belongs to the game version rather than to the schema.
//
// What is left here is the part that belongs to a client: where the column
// sits, which the blob does not say, and the shape the world stores.

// dimensionTypeRegistry is the registry whose entries carry a dimension's
// minimum build height.
const dimensionTypeRegistry = "minecraft:dimension_type"

// splitColumn775 slices a column's blob into one section per byte range the
// shared reader found, keyed by the section's index in the world.
//
// bottom is the index of the column's lowest section, which is the dimension's
// minimum build height divided by sixteen. The blob does not carry it: it is a
// property of the dimension, and the section that a reader thinks is at y=0 is
// wrong by four in the overworld if it assumes otherwise.
//
// Each section's biomes are dropped. The world keeps no biome state, and the
// reader walks them whether or not anyone wants them, because they sit between
// one section's blocks and the next one's.
func splitColumn775(data []byte, bottom int) ([]world.SectionData, error) {
	sections, err := chunk.Split775(data, bottom)
	if err != nil {
		return nil, err
	}

	stored := make([]world.SectionData, 0, len(sections))
	for _, section := range sections {
		stored = append(stored, world.SectionData{
			Y:      section.Y,
			Raw:    section.Blocks,
			Decode: decodeSection775,
		})
	}

	return stored, nil
}

// decodeSection775 is the world's decoder for one section of a 26.1 column.
//
// It is pure, as the world requires: two readers racing to decode the same
// bytes compute the same answer.
//
// A failure is reported in both vocabularies. The world's contract is that a
// section it cannot read answers with ErrSectionNotDecodable rather than with
// air, and the reader's own error says which part of the container it could
// not read; wrapping both keeps a caller matching either one working.
func decodeSection775(raw []byte) ([]uint32, error) {
	states, err := chunk.DecodeSection775(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", world.ErrSectionNotDecodable, err)
	}

	return states, nil
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
