package game

import "sort"

// CanWin 标准 4 面子 + 1 将
func CanWin(hand []Tile) bool {
	if len(hand)%3 != 2 {
		return false
	}
	c := count(hand)
	ids := keys(c)
	for _, id := range ids {
		if c[id] >= 2 {
			c[id] -= 2
			if formMelds(c) {
				c[id] += 2
				return true
			}
			c[id] += 2
		}
	}
	return false
}

func count(hand []Tile) map[int]int {
	m := map[int]int{}
	for _, t := range hand {
		m[t.ID()]++
	}
	return m
}

func keys(m map[int]int) []int {
	var ids []int
	for id, n := range m {
		if n > 0 {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}

func formMelds(c map[int]int) bool {
	ids := keys(c)
	if len(ids) == 0 {
		return true
	}
	id := ids[0]
	n := c[id]

	// 刻子
	if n >= 3 {
		c[id] -= 3
		if formMelds(c) {
			c[id] += 3
			return true
		}
		c[id] += 3
	}

	// 顺子（仅万筒条）
	suit := Suit(id / 10)
	num := id % 10
	if suit <= Tiao && num <= 7 {
		a, b := id+1, id+2
		if c[a] > 0 && c[b] > 0 {
			c[id]--
			c[a]--
			c[b]--
			if formMelds(c) {
				c[id]++
				c[a]++
				c[b]++
				return true
			}
			c[id]++
			c[a]++
			c[b]++
		}
	}
	return false
}

func CanPung(hand []Tile, t Tile) bool {
	n := 0
	for _, h := range hand {
		if h.ID() == t.ID() {
			n++
		}
	}
	return n >= 2
}

// RemoveN 从手牌移除 n 张同 ID 的牌
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
