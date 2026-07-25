package sichuan

import "mahjong/internal/game/universal"

// QueCleared: hand+melds contain no dingque suit
func QueCleared(hand []universal.Tile, melds []universal.Meld, que universal.Suit) bool {
	if que < universal.Wan || que > universal.Tiao {
		return false
	}
	for _, t := range hand {
		if t.Suit == que {
			return false
		}
	}
	for _, m := range melds {
		for _, t := range m.Tiles {
			if t.Suit == que {
				return false
			}
		}
	}
	return true
}

// CanWin: standard shape + dingque cleared
func CanWin(hand []universal.Tile, melds []universal.Meld, que universal.Suit) bool {
	if !universal.CanWinStandard(hand) {
		return false
	}
	return QueCleared(hand, melds, que)
}

// HasQueYiMen: at most two suits among hand+melds
func HasQueYiMen(hand []universal.Tile, melds []universal.Meld) bool {
	seen := map[universal.Suit]bool{}
	for _, t := range hand {
		if t.Suit <= universal.Tiao {
			seen[t.Suit] = true
		}
	}
	for _, m := range melds {
		for _, t := range m.Tiles {
			if t.Suit <= universal.Tiao {
				seen[t.Suit] = true
			}
		}
	}
	return len(seen) <= 2
}