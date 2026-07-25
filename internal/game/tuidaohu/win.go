package tuidaohu

import (
	"sort"

	"mahjong/internal/game/universal"
)

// CanWin: standard / seven pairs / thirteen-bukao
func CanWin(hand []universal.Tile) bool {
	if len(hand)%3 != 2 {
		return false
	}
	if universal.CanWinStandard(hand) {
		return true
	}
	if CanSevenPairs(hand) {
		return true
	}
	if CanThirteenBukao(hand) {
		return true
	}
	return false
}

func CanSevenPairs(hand []universal.Tile) bool {
	if len(hand) != 14 {
		return false
	}
	c := universal.CountTiles(hand)
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

// CanThirteenBukao: each of wan/tong/tiao occupies one of 147/258/369,
// three groups distinct, all singles, exactly 5 distinct honors.
func CanThirteenBukao(hand []universal.Tile) bool {
	if len(hand) != 14 {
		return false
	}
	c := universal.CountTiles(hand)
	for _, n := range c {
		if n > 1 {
			return false
		}
	}

	honorN := 0
	for id := range c {
		suit := universal.Suit(id / 10)
		if suit == universal.Feng || suit == universal.Jian {
			honorN++
		}
	}
	if honorN > 5 {
		return false
	}

	suitNums := map[universal.Suit][]int{}
	for id := range c {
		suit := universal.Suit(id / 10)
		if suit > universal.Tiao {
			continue
		}
		num := id % 10
		suitNums[suit] = append(suitNums[suit], num)
	}
	if len(suitNums[universal.Wan]) == 0 || len(suitNums[universal.Tong]) == 0 || len(suitNums[universal.Tiao]) == 0 {
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
	for _, s := range []universal.Suit{universal.Wan, universal.Tong, universal.Tiao} {
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

func IsQingYiSe(hand []universal.Tile, melds []universal.Meld) bool {
	var suit universal.Suit
	set := false
	check := func(t universal.Tile) bool {
		if t.Suit > universal.Tiao {
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

// PatternMultiplier: seven pairs x2, thirteen x2, qing x6; multiply stack
func PatternMultiplier(hand []universal.Tile, melds []universal.Meld) int {
	mul := 1
	if len(melds) == 0 {
		if CanSevenPairs(hand) {
			mul *= 2
		} else if CanThirteenBukao(hand) {
			mul *= 2
		}
	}
	if IsQingYiSe(hand, melds) {
		mul *= 6
	}
	return mul
}

func SichuanFan(hand []universal.Tile, melds []universal.Meld) int {
	fan := 1
	if CanSevenPairs(hand) {
		fan += 6
	}
	if IsQingYiSe(hand, melds) {
		fan += 6
	}
	if SichuanPungHu(hand, melds) {
		fan += 5
	}
	return fan
}

func SichuanPungHu(hand []universal.Tile, melds []universal.Meld) bool {
	if len(melds) == 0 {
		return false
	}
	if !universal.CanWinStandard(hand) {
		return false
	}
	return true
}
