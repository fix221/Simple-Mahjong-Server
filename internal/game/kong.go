package game

func CountID(hand []Tile, id int) int {
	n := 0
	for _, t := range hand {
		if t.ID() == id {
			n++
		}
	}
	return n
}

func CanAnKong(hand []Tile) []Tile {
	// 返回可暗杠的牌代表（每种 id 一个）
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

// FindPungMeldIndex 找可加杠的碰副露
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
