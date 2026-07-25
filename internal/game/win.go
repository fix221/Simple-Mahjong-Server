package game

import "sort"

// CanWin: standard / seven pairs / thirteen-bukao
func CanWin(hand []Tile) bool {
	if len(hand)%3 != 2 {
		return false
	}
	if CanWinStandard(hand) {
		return true
	}
	if CanSevenPairs(hand) {
		return true
	}
	if CanThirteenOrphansLike(hand) {
		return true
	}
	return false
}

func CanWinStandard(hand []Tile) bool {
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

func CanSevenPairs(hand []Tile) bool {
	if len(hand) != 14 {
		return false
	}
	c := count(hand)
	pairs := 0
	for _, n := range c {
		if n == 0 {
			continue
		}
		if n%2 != 0 {
			return false
		}
		pairs += n / 2
	}
	return pairs == 7
}

// CanThirteenOrphansLike: 147/258/369 each suit once + 5 distinct honors, all singles
func CanThirteenOrphansLike(hand []Tile) bool {
	if len(hand) != 14 {
		return false
	}
	c := count(hand)
	for _, n := range c {
		if n > 1 {
			return false
		}
	}

	honorN := 0
	for id := range c {
		suit := Suit(id / 10)
		if suit == Feng || suit == Jian {
			honorN++
		}
	}
	if honorN > 5 {
		return false
	}

	suitNums := map[Suit][]int{}
	for id := range c {
		suit := Suit(id / 10)
		if suit > Tiao {
			continue
		}
		num := id % 10
		suitNums[suit] = append(suitNums[suit], num)
	}
	if len(suitNums[Wan]) == 0 || len(suitNums[Tong]) == 0 || len(suitNums[Tiao]) == 0 {
		return false
	}

	type groupKey int
	const (
		g147 groupKey = 1
		g258 groupKey = 2
		g369 groupKey = 3
	)
	matchGroup := func(nums []int) (groupKey, bool) {
		if len(nums) != 3 {
			return 0, false
		}
		sort.Ints(nums)
		a, b, c0 := nums[0], nums[1], nums[2]
		if a == 1 && b == 4 && c0 == 7 {
			return g147, true
		}
		if a == 2 && b == 5 && c0 == 8 {
			return g258, true
		}
		if a == 3 && b == 6 && c0 == 9 {
			return g369, true
		}
		return 0, false
	}

	used := map[groupKey]bool{}
	for _, s := range []Suit{Wan, Tong, Tiao} {
		nums := append([]int{}, suitNums[s]...)
		g, ok := matchGroup(nums)
		if !ok {
			return false
		}
		if used[g] {
			return false
		}
		used[g] = true
	}
	if !used[g147] || !used[g258] || !used[g369] {
		return false
	}
	if honorN != 5 {
		return false
	}
	return true
}

func IsQingYiSe(hand []Tile, melds []Meld) bool {
	var suit Suit
	set := false
	check := func(t Tile) bool {
		if t.Suit > Tiao {
			return false
		}
		if !set {
			suit = t.Suit
			set = true
			return true
		}
		return t.Suit == suit
	}
	for _, t := range hand {
		if !check(t) {
			return false
		}
	}
	for _, m := range melds {
		for _, t := range m.Tiles {
			if !check(t) {
				return false
			}
		}
	}
	return set
}

func PatternMultiplier(hand []Tile, melds []Meld) int {
	mul := 1
	if len(melds) == 0 {
		if CanSevenPairs(hand) {
			mul *= 2
		} else if CanThirteenOrphansLike(hand) {
			mul *= 2
		}
	}
	if IsQingYiSe(hand, melds) {
		mul *= 2
	}
	return mul
}

func IsValidHorse(t Tile) bool {
	if t.Suit > Tiao {
		return false
	}
	return t.Num == 1 || t.Num == 5 || t.Num == 9
}

func CountValidHorses(tiles []Tile) int {
	n := 0
	for _, t := range tiles {
		if IsValidHorse(t) {
			n++
		}
	}
	return n
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

	if n >= 3 {
		c[id] -= 3
		if formMelds(c) {
			c[id] += 3
			return true
		}
		c[id] += 3
	}

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