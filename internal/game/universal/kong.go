package universal

// CanAnKong returns one representative tile per kongable id
func CanAnKong(hand []Tile) []Tile {
	cnt := map[int]int{}
	rep := map[int]Tile{}
	for _, t := range hand {
		cnt[t.ID()]++
		rep[t.ID()] = t
	}
	var out []Tile
	for id, n := range cnt {
		if n >= 4 {
			out = append(out, rep[id])
		}
	}
	return out
}

func CanMingKong(hand []Tile, t Tile) bool {
	return CountID(hand, t.ID()) >= 3
}

// FindPungMeldIndex finds pung meld that can be upgraded to jia-kong
func FindPungMeldIndex(melds []Meld, t Tile) int {
	for i, m := range melds {
		if m.Type == MeldPung && len(m.Tiles) > 0 && m.Tiles[0].ID() == t.ID() {
			return i
		}
	}
	return -1
}

func CanJiaKong(hand []Tile, melds []Meld) []Tile {
	var out []Tile
	seen := map[int]bool{}
	for _, t := range hand {
		if seen[t.ID()] {
			continue
		}
		if FindPungMeldIndex(melds, t) >= 0 {
			out = append(out, t)
			seen[t.ID()] = true
		}
	}
	return out
}