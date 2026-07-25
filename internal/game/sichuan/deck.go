package sichuan

import "mahjong/internal/game/universal"

// Deck 108 tiles: wan/tong/tiao only
func Deck() []universal.Tile {
	var d []universal.Tile
	for s := universal.Wan; s <= universal.Tiao; s++ {
		for n := 1; n <= 9; n++ {
			for i := 0; i < 4; i++ {
				d = append(d, universal.Tile{Suit: s, Num: n})
			}
		}
	}
	return d
}

// ExchangeDir: 1 xiajia, 2 duijia, 3 shangjia
func ExchangeDir(diceSum int) int {
	switch diceSum % 3 {
	case 1:
		return 1
	case 2:
		return 3
	default:
		return 2
	}
}