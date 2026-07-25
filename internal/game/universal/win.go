package universal

// CanWinStandard: 4 melds + 1 pair (honors cannot form chow)
func CanWinStandard(hand []Tile) bool {
	if len(hand)%3 != 2 {
		return false
	}
	c := CountTiles(hand)
	ids := SortedIDs(c)
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

func formMelds(c map[int]int) bool {
	ids := SortedIDs(c)
	if len(ids) == 0 {
		return true
	}
	id := ids[0]
	n := c[id]

	// pung
	if n >= 3 {
		c[id] -= 3
		if formMelds(c) {
			c[id] += 3
			return true
		}
		c[id] += 3
	}

	// chow (suits only)
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