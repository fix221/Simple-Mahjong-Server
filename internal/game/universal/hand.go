package universal

import "sort"

func CountID(hand []Tile, id int) int {
	n := 0
	for _, t := range hand {
		if t.ID() == id {
			n++
		}
	}
	return n
}

func CanPung(hand []Tile, t Tile) bool {
	return CountID(hand, t.ID()) >= 2
}

func RemoveN(hand []Tile, t Tile, n int) ([]Tile, bool) {
	out := make([]Tile, 0, len(hand))
	left := n
	for _, h := range hand {
		if left > 0 && h.ID() == t.ID() {
			left--
			continue
		}
		out = append(out, h)
	}
	return out, left == 0
}

func RemoveOne(hand []Tile, t Tile) ([]Tile, bool) {
	return RemoveN(hand, t, 1)
}

func SortHand(hand []Tile) {
	sort.Slice(hand, func(i, j int) bool {
		return hand[i].ID() < hand[j].ID()
	})
}

func CountTiles(hand []Tile) map[int]int {
	m := map[int]int{}
	for _, t := range hand {
		m[t.ID()]++
	}
	return m
}

func SortedIDs(m map[int]int) []int {
	var ids []int
	for id, n := range m {
		if n > 0 {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}