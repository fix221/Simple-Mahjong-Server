package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"mahjong/internal/auth"
	"mahjong/internal/game/sichuan"
	"mahjong/internal/game/tuidaohu"
	"mahjong/internal/game/universal"

	"github.com/gorilla/websocket"
)
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type Msg struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type Player struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Username string          `json:"username,omitempty"`
	UserID   int64           `json:"user_id,omitempty"`
	Gold     int             `json:"gold"`
	Online   bool            `json:"online"`
	Conn     *websocket.Conn `json:"-"`
}

type Room struct {
	ID      string    `json:"id"`
	Rule    string    `json:"rule"`
	Owner   string    `json:"owner"`
	InGame  bool      `json:"in_game"`
	Players []*Player `json:"players"`
	State   *State    `json:"-"`
	Dealer  int       `json:"dealer"`
}

type State struct {
	Rule      string
	Wall      []universal.Tile
	Hands     [][]universal.Tile
	Discards  [][]universal.Tile
	Melds     [][]universal.Meld
	Current   int
	Phase     string // exchange | dingque | discard | wait_action
	LastFrom  int
	LastTile  universal.Tile
	Won       []bool
	Passed    []bool
	NeedAct   []bool
	Exchange  [][]universal.Tile
	Exchanged []bool
	Que       []int
	QueDone   []bool
	PendingJiaKong bool
	Dealer       int
	SealedN      int
	SealedSnap   []universal.Tile
	KongRecords  []tuidaohu.KongRecord
	LastKongKind string
	LastKongFrom int
	WinKind      string
	WinSeat      int
	IsHuang      bool
	BaseScore    int
}

type Hub struct {
	mu       sync.Mutex
	rooms    map[string]*Room
	sessions map[string]*Player
}
var hub = &Hub{rooms: map[string]*Room{}, sessions: map[string]*Player{}}
const defaultGold = 10000
var userStore *auth.Store

func persistGold(p *Player) {
	if p == nil {
		return
	}
	if p.Gold < 0 {
		p.Gold = 0
	}
	if userStore != nil && p.UserID > 0 {
		if err := userStore.SetGold(p.UserID, p.Gold); err != nil {
			log.Printf("persist gold user=%d: %v", p.UserID, err)
		}
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())
	dbPath := os.Getenv("MAHJONG_DB")
	if dbPath == "" {
		dbPath = "mahjong.db"
	}
	st, err := auth.Open(dbPath)
	if err != nil {
		log.Fatalf("sqlite open %s: %v", dbPath, err)
	}
	userStore = st
	defer userStore.Close()
	log.Printf("sqlite: %s", dbPath)
	http.HandleFunc("/ws", onWS)
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	})
	log.Println("Mahjong server :8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

func onWS(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	// 长连接：读超时 + pong 续期；定时 ping
	const pongWait = 90 * time.Second
	const pingPeriod = 30 * time.Second
	c.SetReadLimit(1 << 20)
	_ = c.SetReadDeadline(time.Now().Add(pongWait))
	c.SetPongHandler(func(string) error {
		return c.SetReadDeadline(time.Now().Add(pongWait))
	})
	// session 可在 resume 后切换为房间内原玩家指针
	session := &Player{ID: id6(), Conn: c, Name: "游客", Online: true, Gold: defaultGold}
	hub.mu.Lock()
	hub.sessions[session.ID] = session
	hub.mu.Unlock()
	reply(c, "connected", map[string]any{"id": session.ID, "resume_hint": true, "gold": session.Gold})
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second)); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()
	defer func() {
		close(done)
		onLeave(session, c)
	}()
	for {
		_, b, err := c.ReadMessage()
		if err != nil {
			return
		}
		_ = c.SetReadDeadline(time.Now().Add(pongWait))
		var m Msg
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		if m.Type == "ping" {
			reply(c, "pong", map[string]any{"t": time.Now().Unix()})
			continue
		}
		if m.Type == "resume" {
			if np := tryResume(session, c, m.Data); np != nil {
				session = np
			}
			continue
		}
		handle(session, m)
	}
}

func handle(p *Player, m Msg) {
	switch m.Type {
	case "register":
		doRegister(p, m.Data)
	case "login":
		doLogin(p, m.Data)
	case "set_name":
		var d struct {
			Name string `json:"name"`
		}
		json.Unmarshal(m.Data, &d)
		name := strings.TrimSpace(d.Name)
		if name == "" {
			return
		}
		if len(name) > 24 {
			name = name[:24]
		}
		if p.UserID > 0 {
			p.Name = name
			if userStore != nil {
				_ = userStore.SetNickname(p.UserID, name)
			}
		} else {
			p.Name = name
			if p.Gold <= 0 {
				p.Gold = defaultGold
			}
		}
		hub.mu.Lock()
		hub.sessions[p.ID] = p
		hub.mu.Unlock()
		reply(p.Conn, "profile", map[string]any{
			"name":     p.Name,
			"gold":     p.Gold,
			"username": p.Username,
			"user_id":  p.UserID,
			"guest":    p.UserID == 0,
		})
	case "create_room":
		createRoom(p, m.Data)
	case "join_room":
		joinRoom(p, m.Data)
	case "leave_room":
		leaveRoom(p)
	case "list_rooms":
		listRooms(p)
	case "start_game":
		startGame(p)
	case "discard":
		doDiscard(p, m.Data)
	case "action":
		doAction(p, m.Data)
	case "exchange":
		doExchange(p, m.Data)
	case "self_win":
		doSelfWin(p)
	case "dingque":
		doDingQue(p, m.Data)
	case "kong":
		doKong(p, m.Data)
	case "rob_kong":
		doRobKong(p, m.Data)
	}
}
// tryResume 用旧 player_id 恢复离线座位，成功返回房间内玩家指针

func tryResume(temp *Player, c *websocket.Conn, raw json.RawMessage) *Player {
	var d struct {
		PlayerID string `json:"player_id"`
		Name     string `json:"name"`
	}
	if json.Unmarshal(raw, &d) != nil || strings.TrimSpace(d.PlayerID) == "" {
		reply(c, "resume_fail", map[string]any{"msg": "重连参数无效"})
		return nil
	}
	pid := strings.TrimSpace(d.PlayerID)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	// 1) 全局会话表
	pl := hub.sessions[pid]
	// 2) 房间内再找一遍（防止 sessions 丢失）
	var room *Room
	seat := -1
	if pl == nil {
		for _, r := range hub.rooms {
			for i, x := range r.Players {
				if x != nil && x.ID == pid {
					pl = x
					room = r
					seat = i
					hub.sessions[pid] = x
					break
				}
			}
			if pl != nil {
				break
			}
		}
	} else {
		// 找该玩家所在房间
		for _, r := range hub.rooms {
			for i, x := range r.Players {
				if x != nil && x.ID == pid {
					room = r
					seat = i
					// 保证房间持有同一指针
					r.Players[i] = pl
					break
				}
			}
			if room != nil {
				break
			}
		}
	}
	if pl == nil {
		log.Printf("resume fail: unknown id=%s", pid)
		reply(c, "resume_fail", map[string]any{"msg": "会话已失效，请重新连接"})
		return nil
	}
	// 顶掉旧连接
	if pl.Conn != nil && pl.Conn != c {
		old := pl.Conn
		pl.Conn = nil
		go func(oc *websocket.Conn) { _ = oc.Close() }(old)
	}
	pl.Conn = c
	pl.Online = true
	if strings.TrimSpace(d.Name) != "" {
		pl.Name = strings.TrimSpace(d.Name)
	}
	if userStore != nil && pl.UserID > 0 {
		if u, err := userStore.GetByID(pl.UserID); err == nil {
			pl.Gold = u.Gold
			pl.Name = u.Nickname
			pl.Username = u.Username
		}
	}
	// 临时壳：从 sessions 移除，避免 defer 误伤
	if temp != nil && temp != pl {
		temp.Online = false
		temp.Conn = nil
		if hub.sessions[temp.ID] == temp {
			delete(hub.sessions, temp.ID)
		}
	}
	hub.sessions[pl.ID] = pl
	if room != nil {
		broadcast(room, "player_reconnected", map[string]any{
			"player_id": pl.ID,
			"name":      pl.Name,
			"seat":      seat,
			"online":    true,
		})
		broadcast(room, "tips", map[string]any{"msg": pl.Name + " 已重新连接"})
		broadcast(room, "room", roomView(room))
		pushResumeState(pl, room, seat)
		log.Printf("resume ok id=%s room=%s seat=%d", pl.ID, room.ID, seat)
	} else {
		// 不在房间：仍恢复同一 ID
		reply(c, "resume_ok", map[string]any{
			"id":      pl.ID,
			"name":    pl.Name,
			"gold":    pl.Gold,
			"in_game": false,
			"seat":    -1,
		})
		log.Printf("resume ok id=%s (lobby)", pl.ID)
	}
	return pl
}

func pushResumeState(p *Player, r *Room, seat int) {
	data := map[string]any{
		"id":      p.ID,
		"room":    roomView(r),
		"in_game": r.InGame,
		"seat":    seat,
		"rule":    r.Rule,
		"players": names(r),
	}
	if !r.InGame || r.State == nil {
		reply(p.Conn, "resume_ok", data)
		return
	}
	st := r.State
	// 手牌 + 副露必须一起推，否则碰/杠后会「少牌」
	data["hand"] = st.Hands[seat]
	data["hand_count"] = len(st.Hands[seat])
	data["melds"] = st.Melds[seat]
	allM := make([][]universal.Meld, len(st.Melds))
	for i := range st.Melds {
		if i == seat {
			allM[i] = st.Melds[i]
		} else {
			allM[i] = maskAnKongMelds(st.Melds[i])
		}
	}
	data["all_melds"] = allM
	data["phase"] = st.Phase
	data["current"] = st.Current
	data["wall"] = wallAvail(st)
	data["won"] = st.Won
	if st.Rule == "sichuan" && st.Que != nil && seat < len(st.Que) {
		data["que"] = st.Que[seat]
		data["all_que"] = st.Que
		if st.Exchanged != nil && seat < len(st.Exchanged) {
			data["need_exchange"] = st.Phase == "exchange" && !st.Exchanged[seat]
			data["exchanged"] = st.Exchanged[seat]
		}
		if st.QueDone != nil && seat < len(st.QueDone) {
			data["need_dingque"] = st.Phase == "dingque" && !st.QueDone[seat]
		}
	}
	disc := make([][]universal.Tile, len(st.Discards))
	for i := range st.Discards {
		disc[i] = st.Discards[i]
	}
	data["discards"] = disc
	reply(p.Conn, "resume_ok", data)
	if st.Phase == "discard" && st.Current == seat && !st.Won[seat] {
		// 补发暗杠/加杠/自摸提示
		sendSelfOptions(r, seat)
		reply(p.Conn, "turn", map[string]any{"current": st.Current, "phase": st.Phase, "wall": wallAvail(st)})
	}
	if st.Phase == "wait_action" && st.NeedAct != nil && seat < len(st.NeedAct) && st.NeedAct[seat] && (st.Passed == nil || !st.Passed[seat]) {
		// 重发操作提示
		tile := st.LastTile
		canP := universal.CanPung(st.Hands[seat], tile)
		canK := universal.CanMingKong(st.Hands[seat], tile)
		canW := false
		cand := append(append([]universal.Tile{}, st.Hands[seat]...), tile)
		if st.Rule == "sichuan" {
			if st.Que[seat] >= 0 {
				canW = sichuan.CanWin(cand, st.Melds[seat], universal.Suit(st.Que[seat]))
			}
			if st.Que[seat] >= 0 && int(tile.Suit) == st.Que[seat] {
				canP = false
				canK = false
			}
		} else {
			// 推倒胡不可点炮
			canW = false
		}
		if canP || canK || canW {
			reply(p.Conn, "action_prompt", map[string]any{
				"tile": tile, "from": st.LastFrom,
				"can_pung": canP, "can_kong": canK, "can_win": canW, "self": false,
			})
		}
	}
}

func createRoom(p *Player, raw json.RawMessage) {
	var d struct {
		Rule string `json:"rule"`
	}
	json.Unmarshal(raw, &d)
	if d.Rule != "sichuan" {
		d.Rule = "tuidaohu"
	}
	leaveRoom(p)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := &Room{ID: id4(), Rule: d.Rule, Owner: p.ID, Players: []*Player{p}}
	hub.rooms[r.ID] = r
	hub.sessions[p.ID] = p
	reply(p.Conn, "room", roomView(r))
}

func joinRoom(p *Player, raw json.RawMessage) {
	var d struct {
		RoomID string `json:"room_id"`
	}
	json.Unmarshal(raw, &d)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := hub.rooms[d.RoomID]
	if r == nil {
		reply(p.Conn, "error", map[string]string{"msg": "房间不存在"})
		return
	}
	if r.InGame {
		reply(p.Conn, "error", map[string]string{"msg": "已在对局中"})
		return
	}
	if len(r.Players) >= 4 {
		reply(p.Conn, "error", map[string]string{"msg": "房间已满"})
		return
	}
	for _, x := range r.Players {
		if x.ID == p.ID {
			reply(p.Conn, "room", roomView(r))
			return
		}
	}
	removeFromRoomsLocked(p, "leave", p.Conn)
	r.Players = append(r.Players, p)
	hub.sessions[p.ID] = p
	broadcast(r, "room", roomView(r))
}

func removeFromRoomsLocked(p *Player, reason string, closing *websocket.Conn) {
	for id, r := range hub.rooms {
		for i, x := range r.Players {
			if x.ID != p.ID {
				continue
			}
			// 重连竞态：该座位已被新连接接管，旧连接的 leave 忽略
			if closing != nil && x.Conn != nil && x.Conn != closing {
				return
			}
			name := p.Name
			seat := i
			// 断线：一律保留座位，仅标离线（大厅/对局都可 resume）
			if reason == "disconnect" {
				x.Online = false
				if x.Conn == closing || x.Conn == nil {
					x.Conn = nil
				}
				broadcast(r, "player_left", map[string]any{
					"player_id": p.ID,
					"name":      name,
					"seat":      seat,
					"reason":    "disconnect",
					"in_game":   r.InGame,
					"offline":   true,
					"owner":     r.Owner,
				})
				broadcast(r, "tips", map[string]any{
					"msg": name + " 断线了，等待重连",
				})
				broadcast(r, "room", roomView(r))
				// 对局中若轮到他，跳过以免卡死
				if r.InGame && r.State != nil && r.State.Phase == "discard" && r.State.Current == seat {
					nextTurn(r, seat)
				}
				return
			}
			// 主动离开：移除
			r.Players = append(r.Players[:i], r.Players[i+1:]...)
			if len(r.Players) == 0 {
				delete(hub.rooms, id)
				return
			}
			if r.Owner == p.ID {
				r.Owner = r.Players[0].ID
			}
			broadcast(r, "player_left", map[string]any{
				"player_id": p.ID,
				"name":      name,
				"seat":      seat,
				"reason":    reason,
				"in_game":   r.InGame,
				"offline":   false,
				"owner":     r.Owner,
			})
			broadcast(r, "room", roomView(r))
			broadcast(r, "tips", map[string]any{"msg": name + " 离开了房间"})
			return
		}
	}
}

func leaveRoom(p *Player) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	removeFromRoomsLocked(p, "leave", p.Conn)
}
// onLeave closing 为正在关闭的连接；若座位已被新连接接管则不处理

func onLeave(p *Player, closing *websocket.Conn) {
	hub.mu.Lock()
	if p != nil && p.Conn != nil && closing != nil && p.Conn != closing {
		hub.mu.Unlock()
		return
	}
	// 若 sessions 里已是别的指针/新连接，忽略
	if p != nil {
		if cur, ok := hub.sessions[p.ID]; ok && cur != nil && cur.Conn != nil && closing != nil && cur.Conn != closing {
			hub.mu.Unlock()
			return
		}
		p.Online = false
		if p.Conn == closing {
			p.Conn = nil
		}
		// 保留 sessions[p.ID]，ID 可被 resume
	}
	removeFromRoomsLocked(p, "disconnect", closing)
	hub.mu.Unlock()
	if closing != nil {
		_ = closing.Close()
	}
}

func listRooms(p *Player) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	type item struct {
		ID     string `json:"id"`
		Rule   string `json:"rule"`
		Count  int    `json:"count"`
		InGame bool   `json:"in_game"`
	}
	list := make([]item, 0)
	for _, r := range hub.rooms {
		list = append(list, item{ID: r.ID, Rule: r.Rule, Count: len(r.Players), InGame: r.InGame})
	}
	reply(p.Conn, "room_list", list)
}

func startGame(p *Player) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.Owner != p.ID {
		return
	}
	if len(r.Players) < 2 {
		reply(p.Conn, "error", map[string]string{"msg": "至少 2 人"})
		return
	}
	n := len(r.Players)
	var deck []universal.Tile
	if r.Rule == "sichuan" {
		deck = sichuan.Deck()
	} else {
		deck = universal.FullDeck()
	}
	universal.Shuffle(deck)
	st := &State{
		Rule: r.Rule, Hands: make([][]universal.Tile, n), Discards: make([][]universal.Tile, n),
		Melds: make([][]universal.Meld, n), Won: make([]bool, n),
		Exchange: make([][]universal.Tile, n), Exchanged: make([]bool, n),
		Que: make([]int, n), QueDone: make([]bool, n),
	}
	for i := 0; i < n; i++ {
		st.Que[i] = -1
	}
	for i := 0; i < n; i++ {
		st.Hands[i] = append([]universal.Tile{}, deck[:13]...)
		deck = deck[13:]
		universal.SortHand(st.Hands[i])
	}
	st.Wall = deck
	if r.Dealer < 0 || r.Dealer >= n {
		r.Dealer = 0
	}
	st.Dealer = r.Dealer
	st.BaseScore = 1
	st.WinSeat = -1
	st.LastKongFrom = -1
	if r.Rule != "sichuan" {
		st.SealedN = 6
		if len(st.Wall) >= 6 {
			st.SealedSnap = append([]universal.Tile{}, st.Wall[len(st.Wall)-6:]...)
		} else {
			st.SealedSnap = append([]universal.Tile{}, st.Wall...)
			st.SealedN = len(st.Wall)
		}
	}
	r.State = st
	r.InGame = true
	if r.Rule == "sichuan" {
		st.Phase = "exchange"
		for i, pl := range r.Players {
			reply(pl.Conn, "game_start", map[string]any{
				"seat": i, "rule": r.Rule, "hand": st.Hands[i], "exchange": true, "players": names(r),
			})
		}
		return
	}
	st.Current = st.Dealer
	st.Phase = "discard"
	_ = drawOne(st, st.Dealer)
	for i, pl := range r.Players {
		reply(pl.Conn, "game_start", map[string]any{
			"seat": i, "rule": r.Rule, "hand": st.Hands[i], "exchange": false, "players": names(r),
			"dealer": st.Dealer, "sealed": st.SealedN,
		})
	}
	broadcast(r, "turn", map[string]any{"current": st.Current, "phase": st.Phase, "wall": wallAvail(st)})
	sendSelfOptions(r, st.Dealer)
}

func doExchange(p *Player, raw json.RawMessage) {
	var wrap struct {
		Tiles json.RawMessage `json:"tiles"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		reply(p.Conn, "error", map[string]string{"msg": "数据格式错误"})
		return
	}
	tiles, err := universal.ParseTiles(wrap.Tiles)
	if err != nil || len(tiles) != 3 {
		reply(p.Conn, "error", map[string]string{"msg": "必须选 3 张有效牌"})
		return
	}
	suit := tiles[0].Suit
	for _, t := range tiles {
		if t.Suit != suit || t.Suit > universal.Tiao {
			reply(p.Conn, "error", map[string]string{"msg": "换三张须同花色"})
			return
		}
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil || r.State.Phase != "exchange" {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || st.Exchanged[seat] {
		return
	}
	hand := append([]universal.Tile{}, st.Hands[seat]...)
	for _, t := range tiles {
		var ok bool
		hand, ok = universal.RemoveOne(hand, t)
		if !ok {
			reply(p.Conn, "error", map[string]string{"msg": "手牌不足"})
			return
		}
	}
	st.Hands[seat] = hand
	st.Exchange[seat] = tiles
	st.Exchanged[seat] = true
	reply(p.Conn, "exchange_ok", map[string]any{"ok": true})
	for i := 0; i < len(r.Players); i++ {
		if !st.Exchanged[i] {
			return
		}
	}
	dice := rand.Intn(6) + rand.Intn(6) + 2
	dir := sichuan.ExchangeDir(dice)
	n := len(r.Players)
	newHands := make([][]universal.Tile, n)
	for i := 0; i < n; i++ {
		from := (i - dir + n*3) % n
		newHands[i] = append(st.Hands[i], st.Exchange[from]...)
		universal.SortHand(newHands[i])
	}
	st.Hands = newHands
	// 记录每人新换入的 3 张供客户端高亮
	received := make([][]universal.Tile, n)
	for i := 0; i < n; i++ {
		from := (i - dir + n*3) % n
		received[i] = append([]universal.Tile{}, st.Exchange[from]...)
	}
	st.Phase = "dingque"
	for i, pl := range r.Players {
		reply(pl.Conn, "exchange_done", map[string]any{
			"hand": st.Hands[i], "direction": dir, "dice": dice,
			"received": received[i], "need_dingque": true,
		})
	}
}

func doDingQue(p *Player, raw json.RawMessage) {
	var d struct {
		Suit int `json:"suit"` // 0万 1筒 2条
	}
	if json.Unmarshal(raw, &d) != nil || d.Suit < 0 || d.Suit > 2 {
		reply(p.Conn, "error", map[string]string{"msg": "定缺须选万/筒/条"})
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil || r.State.Phase != "dingque" {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || st.QueDone[seat] {
		return
	}
	st.Que[seat] = d.Suit
	st.QueDone[seat] = true
	reply(p.Conn, "dingque_ok", map[string]any{"suit": d.Suit})
	broadcast(r, "dingque_update", map[string]any{"seat": seat, "suit": d.Suit})
	for i := 0; i < len(r.Players); i++ {
		if !st.QueDone[i] {
			return
		}
	}
	// 全部定缺完毕，庄家摸牌开始
	st.Phase = "discard"
	st.Current = 0
	_ = drawOne(st, 0)
	ques := make([]int, len(r.Players))
	copy(ques, st.Que)
	for i, pl := range r.Players {
		reply(pl.Conn, "game_ready", map[string]any{
			"hand": st.Hands[i], "que": st.Que[i], "all_que": ques,
			"draw": st.Hands[i][len(st.Hands[i])-1],
		})
	}
	broadcast(r, "turn", map[string]any{"current": st.Current, "phase": st.Phase, "wall": wallAvail(st)})
}

func doDiscard(p *Player, raw json.RawMessage) {
	var wrap struct {
		Tile json.RawMessage `json:"tile"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return
	}
	tile, err := universal.ParseTile(wrap.Tile)
	if err != nil {
		reply(p.Conn, "error", map[string]string{"msg": "无效的牌"})
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || seat != st.Current || st.Phase != "discard" || st.Won[seat] {
		reply(p.Conn, "error", map[string]string{"msg": "还没轮到你出牌"})
		return
	}
	// 川麻：手中还有定缺花色时，必须先打定缺
	if st.Rule == "sichuan" && st.Que[seat] >= 0 {
		hasQue := false
		for _, h := range st.Hands[seat] {
			if int(h.Suit) == st.Que[seat] {
				hasQue = true
				break
			}
		}
		if hasQue && int(tile.Suit) != st.Que[seat] {
			reply(p.Conn, "error", map[string]string{"msg": "须先打出定缺花色的牌"})
			return
		}
	}
	hand, ok := universal.RemoveOne(st.Hands[seat], tile)
	if !ok {
		reply(p.Conn, "error", map[string]string{"msg": "手牌中没有这张牌"})
		return
	}
	st.Hands[seat] = hand
	universal.SortHand(st.Hands[seat])
	st.Discards[seat] = append(st.Discards[seat], tile)
	st.LastFrom = seat
	st.LastTile = tile
	st.LastKongKind = ""
	st.LastKongFrom = -1
	st.Phase = "wait_action"
	st.Passed = make([]bool, len(r.Players))
	st.NeedAct = make([]bool, len(r.Players))
	broadcast(r, "discarded", map[string]any{"seat": seat, "tile": tile})
	reply(p.Conn, "hand", map[string]any{"hand": st.Hands[seat], "melds": st.Melds[seat]})
	need := false
	for i := 0; i < len(r.Players); i++ {
		if i == seat || st.Won[i] {
			continue
		}
		cand := append(append([]universal.Tile{}, st.Hands[i]...), tile)
		canP := universal.CanPung(st.Hands[i], tile)
		// 推倒胡只允许自摸；四川可点炮且必须已定缺、手牌无定缺花色
		canW := false
		if st.Rule == "sichuan" && st.Que[i] >= 0 {
			canW = sichuan.CanWin(cand, st.Melds[i], universal.Suit(st.Que[i]))
		}
		canK := universal.CanMingKong(st.Hands[i], tile)
		// 四川：不能碰/杠定缺花色
		if st.Rule == "sichuan" && st.Que[i] >= 0 && int(tile.Suit) == st.Que[i] {
			canP = false
			canK = false
		}
		if canP || canK || canW {
			need = true
			st.NeedAct[i] = true
			reply(r.Players[i].Conn, "action_prompt", map[string]any{
				"tile": tile, "from": seat,
				"can_pung": canP, "can_kong": canK, "can_win": canW, "self": false,
			})
		}
	}
	if !need {
		nextTurn(r, seat)
	}
}

func doAction(p *Player, raw json.RawMessage) {
	var d struct {
		Action string `json:"action"`
	}
	json.Unmarshal(raw, &d)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil || r.State.Phase != "wait_action" {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || st.Won[seat] || !st.NeedAct[seat] {
		return
	}
	switch d.Action {
	case "pung":
		if !universal.CanPung(st.Hands[seat], st.LastTile) {
			return
		}
		hand, ok := universal.RemoveN(st.Hands[seat], st.LastTile, 2)
		if !ok {
			return
		}
		st.Hands[seat] = hand
		t := st.LastTile
		st.Melds[seat] = append(st.Melds[seat], universal.Meld{Type: universal.MeldPung, Tiles: []universal.Tile{t, t, t}})
		st.Current = seat
		st.Phase = "discard"
		if len(st.Discards[st.LastFrom]) > 0 {
			st.Discards[st.LastFrom] = st.Discards[st.LastFrom][:len(st.Discards[st.LastFrom])-1]
		}
		broadcast(r, "pung", map[string]any{"seat": seat, "tile": t, "from": st.LastFrom, "melds": st.Melds[seat]})
		reply(p.Conn, "hand", map[string]any{"hand": st.Hands[seat], "melds": st.Melds[seat]})
		sendSelfOptions(r, seat)
		broadcast(r, "turn", map[string]any{"current": st.Current, "phase": st.Phase, "wall": wallAvail(st)})
	case "kong":
		// 别人弃牌后的直杠（点杠），不可抢杠
		if !universal.CanMingKong(st.Hands[seat], st.LastTile) {
			return
		}
		t := st.LastTile
		if st.Rule == "sichuan" && st.Que[seat] >= 0 && int(t.Suit) == st.Que[seat] {
			return
		}
		hand, ok := universal.RemoveN(st.Hands[seat], t, 3)
		if !ok {
			return
		}
		st.Hands[seat] = hand
		st.Melds[seat] = append(st.Melds[seat], universal.Meld{
			Type: universal.MeldMingKong, Tiles: []universal.Tile{t, t, t, t},
		})
		fromSeat := st.LastFrom
		st.KongRecords = append(st.KongRecords, tuidaohu.KongRecord{Seat: seat, Kind: "ming", From: fromSeat, TileID: t.ID()})
		st.LastKongKind = "ming"
		st.LastKongFrom = fromSeat
		if len(st.Discards[fromSeat]) > 0 {
			st.Discards[fromSeat] = st.Discards[fromSeat][:len(st.Discards[fromSeat])-1]
		}
		broadcast(r, "kong", map[string]any{
			"seat": seat, "tile": t, "from": fromSeat, "kind": "ming",
			"melds": st.Melds[seat],
		})
		afterKongDraw(r, seat)
	case "win":
		// tuidaohu: only rob jia-kong; no normal ron. sichuan: ron ok
		if st.Rule != "sichuan" && !st.PendingJiaKong {
			reply(p.Conn, "error", map[string]string{"msg": "tuidaohu: self-draw or rob-kong only"})
			return
		}
		cand := append(append([]universal.Tile{}, st.Hands[seat]...), st.LastTile)
		if st.Rule == "sichuan" {
			if st.Que[seat] < 0 {
				reply(p.Conn, "error", map[string]string{"msg": "dingque first"})
				return
			}
			if !sichuan.CanWin(cand, st.Melds[seat], universal.Suit(st.Que[seat])) {
				reply(p.Conn, "error", map[string]string{"msg": "cannot win"})
				return
			}
		} else {
			if !tuidaohu.CanWin(cand) {
				reply(p.Conn, "error", map[string]string{"msg": "cannot win"})
				return
			}
		}
		if st.PendingJiaKong {
			fs := st.LastFrom
			if fs >= 0 && fs < len(st.Melds) {
				for mi, m := range st.Melds[fs] {
					if (m.Type == universal.MeldJiaKong) && len(m.Tiles) > 0 && m.Tiles[0].ID() == st.LastTile.ID() {
						st.Melds[fs][mi] = universal.Meld{Type: universal.MeldPung, Tiles: []universal.Tile{st.LastTile, st.LastTile, st.LastTile}}
						break
					}
				}
			}
			// jia kong cancelled: drop fee record for this jia if last record matches
			if n := len(st.KongRecords); n > 0 {
				last := st.KongRecords[n-1]
				if last.Kind == "jia" && last.Seat == fs && last.TileID == st.LastTile.ID() {
					st.KongRecords = st.KongRecords[:n-1]
				}
			}
			st.PendingJiaKong = false
			st.WinKind = "rob_jia"
		} else {
			st.WinKind = "ron"
		}
		st.Hands[seat] = cand
		st.Won[seat] = true
		st.WinSeat = seat
		cont := st.Rule == "sichuan"
		broadcast(r, "won", map[string]any{
			"seat": seat, "from": st.LastFrom, "hand": st.Hands[seat],
			"continue": cont, "won_seats": wonSeatList(st), "win_kind": st.WinKind,
		})
		if st.Rule == "sichuan" {
			afterSichuanWin(r, st.LastFrom)
		} else {
			endGame(r)
		}
	case "pass":
		st.Passed[seat] = true
		st.NeedAct[seat] = false
		for i := range st.NeedAct {
			if st.NeedAct[i] && !st.Passed[i] {
				return
			}
		}
		if st.PendingJiaKong {
			st.PendingJiaKong = false
			afterKongDraw(r, st.LastFrom)
			return
		}
		nextTurn(r, st.LastFrom)
	}
}

func doSelfWin(p *Player) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || seat != st.Current || st.Phase != "discard" || st.Won[seat] {
		return
	}
	if st.Rule == "sichuan" {
		if st.Que[seat] < 0 {
			reply(p.Conn, "error", map[string]string{"msg": "请先定缺"})
			return
		}
		if !sichuan.CanWin(st.Hands[seat], st.Melds[seat], universal.Suit(st.Que[seat])) {
			reply(p.Conn, "error", map[string]string{"msg": "未定缺干净或牌型不能胡"})
			return
		}
	} else if !tuidaohu.CanWin(st.Hands[seat]) {
		return
	}
	st.Won[seat] = true
	st.WinSeat = seat
	// self-draw: if after ming-kong draw => ming_flower (full package); else self
	if st.Rule != "sichuan" && st.LastKongKind == "ming" {
		st.WinKind = "ming_flower"
	} else {
		st.WinKind = "self"
	}
	cont := st.Rule == "sichuan"
	broadcast(r, "won", map[string]any{
		"seat": seat, "from": -1, "hand": st.Hands[seat],
		"continue": cont, "won_seats": wonSeatList(st),
		"win_kind": st.WinKind, "kong_kind": st.LastKongKind,
	})
	if st.Rule == "sichuan" {
		afterSichuanWin(r, seat)
	} else {
		endGame(r)
	}
}

func nextTurn(r *Room, after int) {
	st := r.State
	n := len(r.Players)
	for k := 1; k <= n; k++ {
		i := (after + k) % n
		if st.Won[i] {
			continue
		}
		// 离线座位跳过（等待其重连期间不卡死整桌）
		if i < len(r.Players) && r.Players[i] != nil && !r.Players[i].Online {
			continue
		}
		if wallAvail(st) == 0 {
			st.IsHuang = true
			endGame(r)
			return
		}
		if !drawOne(st, i) {
			st.IsHuang = true
			endGame(r)
			return
		}
		st.Current = i
		st.Phase = "discard"
		reply(r.Players[i].Conn, "draw", map[string]any{
			"tile": st.Hands[i][len(st.Hands[i])-1], "hand": st.Hands[i], "melds": st.Melds[i],
		})
		broadcast(r, "turn", map[string]any{"current": st.Current, "phase": st.Phase, "wall": wallAvail(st)})
		sendSelfOptions(r, i)
		return
	}
	endGame(r)
}
// sichuanShouldEnd 血战：未胡人数 <=1，或已有3人胡（4人标准）

func wonSeatList(st *State) []int {
	out := make([]int, 0)
	for i, w := range st.Won {
		if w {
			out = append(out, i)
		}
	}
	return out
}

func sichuanShouldEnd(st *State, n int) bool {
	wonCount := 0
	for _, w := range st.Won {
		if w {
			wonCount++
		}
	}
	alive := n - wonCount
	if alive <= 1 {
		return true
	}
	// 4 人局经典：3 人胡完结束
	if n >= 4 && wonCount >= 3 {
		return true
	}
	return false
}

func afterSichuanWin(r *Room, afterSeat int) {
	st := r.State
	n := len(r.Players)
	// 血战：有人胡后继续，直到只剩 1 人未胡或 3 人已胡
	st.Phase = "discard"
	st.NeedAct = make([]bool, n)
	st.Passed = make([]bool, n)
	st.PendingJiaKong = false
	r.InGame = true
	if sichuanShouldEnd(st, n) {
		endGame(r)
		return
	}
	broadcast(r, "tips", map[string]any{
		"msg": fmt.Sprintf("血战继续（已胡 %d 人）", len(wonSeatList(st))),
	})
	nextTurn(r, afterSeat)
}
// sendSelfOptions 轮到自己时：自摸胡 / 暗杠 / 加杠
// sendSelfOptions 轮到自己时：自摸胡 / 暗杠 / 加杠

func sendSelfOptions(r *Room, seat int) {
	st := r.State
	if st == nil || seat < 0 || seat >= len(r.Players) {
		return
	}
	pl := r.Players[seat]
	if pl == nil || pl.Conn == nil || !pl.Online || st.Won[seat] {
		return
	}
	canWin := false
	if st.Rule == "sichuan" {
		if st.Que[seat] >= 0 {
			canWin = sichuan.CanWin(st.Hands[seat], st.Melds[seat], universal.Suit(st.Que[seat]))
		}
	} else {
		canWin = tuidaohu.CanWin(st.Hands[seat])
	}
	an := universal.CanAnKong(st.Hands[seat])
	jia := universal.CanJiaKong(st.Hands[seat], st.Melds[seat])
	if st.Rule == "sichuan" && st.Que[seat] >= 0 {
		var an2, jia2 []universal.Tile
		for _, t := range an {
			if int(t.Suit) != st.Que[seat] {
				an2 = append(an2, t)
			}
		}
		for _, t := range jia {
			if int(t.Suit) != st.Que[seat] {
				jia2 = append(jia2, t)
			}
		}
		an, jia = an2, jia2
	}
	if !canWin && len(an) == 0 && len(jia) == 0 {
		return
	}
	reply(pl.Conn, "self_options", map[string]any{
		"can_win":  canWin,
		"an_kong":  an,
		"jia_kong": jia,
		"hand":     st.Hands[seat],
		"melds":    st.Melds[seat],
	})
	if canWin {
		reply(pl.Conn, "action_prompt", map[string]any{
			"tile": st.Hands[seat][len(st.Hands[seat])-1], "from": -1,
			"can_pung": false, "can_kong": false, "can_win": true, "self": true,
		})
	}
}

func afterKongDraw(r *Room, seat int) {
	st := r.State
	// kong supplement: always last tile of entire wall (may use sealed)
	if len(st.Wall) == 0 {
		st.IsHuang = true
		endGame(r)
		return
	}
	t := st.Wall[len(st.Wall)-1]
	st.Wall = st.Wall[:len(st.Wall)-1]
	if st.SealedN > 0 {
		st.SealedN--
	}
	st.Hands[seat] = append(st.Hands[seat], t)
	st.Current = seat
	st.Phase = "discard"
	st.PendingJiaKong = false
	st.NeedAct = make([]bool, len(r.Players))
	st.Passed = make([]bool, len(r.Players))
	// LastKongKind should already be set by caller; keep if set
	reply(r.Players[seat].Conn, "draw", map[string]any{
		"tile": t, "hand": st.Hands[seat], "melds": st.Melds[seat], "kong_draw": true,
		"kong_kind": st.LastKongKind,
	})
	broadcast(r, "turn", map[string]any{"current": seat, "phase": "discard", "wall": wallAvail(st)})
	sendSelfOptions(r, seat)
}
// doKong 自己回合暗杠/加杠: {kind:"an"|"jia", tile:{suit,num}}
// 明杠走 doAction action=kong

func doKong(p *Player, raw json.RawMessage) {
	var d struct {
		Kind string          `json:"kind"`
		Tile json.RawMessage `json:"tile"`
	}
	if json.Unmarshal(raw, &d) != nil {
		return
	}
	tile, err := universal.ParseTile(d.Tile)
	if err != nil {
		reply(p.Conn, "error", map[string]string{"msg": "无效的牌"})
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || st.Won[seat] {
		return
	}
	if seat != st.Current || st.Phase != "discard" {
		reply(p.Conn, "error", map[string]string{"msg": "还没轮到你杠"})
		return
	}
	if st.Rule == "sichuan" && st.Que[seat] >= 0 && int(tile.Suit) == st.Que[seat] {
		reply(p.Conn, "error", map[string]string{"msg": "定缺花色不能杠"})
		return
	}
	switch d.Kind {
	case "an":
		if universal.CountID(st.Hands[seat], tile.ID()) < 4 {
			reply(p.Conn, "error", map[string]string{"msg": "cannot an-kong"})
			return
		}
		hand, ok := universal.RemoveN(st.Hands[seat], tile, 4)
		if !ok {
			return
		}
		st.Hands[seat] = hand
		st.Melds[seat] = append(st.Melds[seat], universal.Meld{
			Type: universal.MeldAnKong, Tiles: []universal.Tile{tile, tile, tile, tile},
		})
		st.KongRecords = append(st.KongRecords, tuidaohu.KongRecord{Seat: seat, Kind: "an", From: -1, TileID: tile.ID()})
		st.LastKongKind = "an"
		st.LastKongFrom = -1
		// public: show 2 tiles only; private full melds
		pubMelds := maskAnKongMelds(st.Melds[seat])
		broadcast(r, "kong", map[string]any{
			"seat": seat, "tile": tile, "from": -1, "kind": "an",
			"melds": pubMelds, "show_tiles": 2, "hidden_tiles": 2,
		})
		reply(p.Conn, "hand", map[string]any{"hand": st.Hands[seat], "melds": st.Melds[seat]})
		afterKongDraw(r, seat)
	case "jia":
		idx := universal.FindPungMeldIndex(st.Melds[seat], tile)
		if idx < 0 || universal.CountID(st.Hands[seat], tile.ID()) < 1 {
			reply(p.Conn, "error", map[string]string{"msg": "cannot jia-kong"})
			return
		}
		hand, ok := universal.RemoveOne(st.Hands[seat], tile)
		if !ok {
			return
		}
		st.Hands[seat] = hand
		st.Melds[seat][idx] = universal.Meld{
			Type: universal.MeldJiaKong, Tiles: []universal.Tile{tile, tile, tile, tile},
		}
		st.KongRecords = append(st.KongRecords, tuidaohu.KongRecord{Seat: seat, Kind: "jia", From: -1, TileID: tile.ID()})
		st.LastKongKind = "jia"
		st.LastKongFrom = -1
		st.LastFrom = seat
		st.LastTile = tile
		st.Phase = "wait_action"
		st.PendingJiaKong = true
		st.Passed = make([]bool, len(r.Players))
		st.NeedAct = make([]bool, len(r.Players))
		broadcast(r, "kong", map[string]any{
			"seat": seat, "tile": tile, "from": seat, "kind": "jia",
			"melds": st.Melds[seat], "robbable": true,
		})
		reply(p.Conn, "hand", map[string]any{"hand": st.Hands[seat], "melds": st.Melds[seat]})
		need := false
		for i := 0; i < len(r.Players); i++ {
			if i == seat || st.Won[i] {
				continue
			}
			cand := append(append([]universal.Tile{}, st.Hands[i]...), tile)
			canW := false
			if st.Rule == "sichuan" && st.Que[i] >= 0 {
				canW = sichuan.CanWin(cand, st.Melds[i], universal.Suit(st.Que[i]))
			} else {
				canW = tuidaohu.CanWin(cand)
			}
			if canW {
				need = true
				st.NeedAct[i] = true
				reply(r.Players[i].Conn, "action_prompt", map[string]any{
					"tile": tile, "from": seat,
					"can_pung": false, "can_kong": false, "can_win": true,
					"self": false, "rob_kong": true,
				})
			}
		}
		if !need {
			afterKongDraw(r, seat)
		}
	default:
		reply(p.Conn, "error", map[string]string{"msg": "未知杠类型"})
	}
}
// wallAvail: tiles before sealed zone for normal draws

func wallAvail(st *State) int {
	if st == nil {
		return 0
	}
	n := len(st.Wall) - st.SealedN
	if n < 0 {
		return 0
	}
	return n
}
// drawOne normal draw from front; never takes sealed

func drawOne(st *State, seat int) bool {
	if wallAvail(st) == 0 {
		return false
	}
	st.Hands[seat] = append(st.Hands[seat], st.Wall[0])
	st.Wall = st.Wall[1:]
	return true
}
// maskAnKongMelds: for public view, an-kong shows only 2 tiles

func maskAnKongMelds(melds []universal.Meld) []universal.Meld {
	out := make([]universal.Meld, len(melds))
	for i, m := range melds {
		if m.Type == universal.MeldAnKong && len(m.Tiles) >= 2 {
			out[i] = universal.Meld{Type: m.Type, Tiles: []universal.Tile{m.Tiles[0], m.Tiles[1]}}
		} else {
			out[i] = m
		}
	}
	return out
}

func doRobKong(p *Player, raw json.RawMessage) {
	// alias: treat as action win during pending jia kong
	var d struct {
		Action string `json:"action"`
	}
	_ = json.Unmarshal(raw, &d)
	if d.Action == "" || d.Action == "win" {
		doAction(p, json.RawMessage(`{"action":"win"}`))
		return
	}
	if d.Action == "pass" {
		doAction(p, json.RawMessage(`{"action":"pass"}`))
	}
}

func endGame(r *Room) {
	st := r.State
	n := len(r.Players)
	base := st.BaseScore
	if base <= 0 {
		base = 1
	}
	// kong fees always settle (including huang)
	kongDelta := tuidaohu.ScoreKongFees(n, st.KongRecords)
	winDelta := make([]int, n)
	horseTiles := []universal.Tile{}
	horseFan := 0
	patternMul := 1
	totalMul := 0
	detail := map[string]any{}
	if !st.IsHuang && st.WinSeat >= 0 && st.WinSeat < n && st.Won[st.WinSeat] {
		ws := st.WinSeat
		// horses
		if st.Rule != "sichuan" {
			if st.LastKongKind == "an" && (st.WinKind == "self" || st.WinKind == "") {
				// an-kong flower: sealed snapshot
				horseTiles = append([]universal.Tile{}, st.SealedSnap...)
			} else {
				// front 6 of remaining wall (or less)
				take := 6
				if len(st.Wall) < take {
					take = len(st.Wall)
				}
				if take > 0 {
					horseTiles = append([]universal.Tile{}, st.Wall[:take]...)
				}
			}
			horseFan = tuidaohu.CountValidHorses(horseTiles)
			patternMul = tuidaohu.PatternMultiplier(st.Hands[ws], st.Melds[ws])
			totalMul = tuidaohu.TotalMultiplier(horseFan, patternMul)
			pay := base * totalMul
			switch st.WinKind {
			case "ming_flower", "rob_jia":
				// full package x3 by responsible seat
				payer := -1
				if st.WinKind == "ming_flower" {
					payer = st.LastKongFrom
				} else {
					payer = st.LastFrom
				}
				if payer >= 0 && payer < n && payer != ws {
					winDelta[payer] -= pay * 3
					winDelta[ws] += pay * 3
				} else {
					// fallback share
					for i := 0; i < n; i++ {
						if i == ws {
							continue
						}
						winDelta[i] -= pay
						winDelta[ws] += pay
					}
				}
			default:
				// normal self-draw: each of others pays base*mul
				for i := 0; i < n; i++ {
					if i == ws {
						continue
					}
					winDelta[i] -= pay
					winDelta[ws] += pay
				}
			}
		}
		detail = map[string]any{
			"win_seat":     ws,
			"win_kind":     st.WinKind,
			"horse_tiles":  horseTiles,
			"horse_fan":    horseFan,
			"pattern_mul":  patternMul,
			"total_mul":    totalMul,
			"base":         base,
			"last_kong":    st.LastKongKind,
		}
	}
	type row struct {
		Name      string      `json:"name"`
		Seat      int         `json:"seat"`
		Won       bool        `json:"won"`
		Hand      []universal.Tile `json:"hand"`
		KongDelta int         `json:"kong_delta"`
		WinDelta  int         `json:"win_delta"`
		Delta     int         `json:"delta"`
		Gold      int         `json:"gold"`
	}
	var res []row
	for i, pl := range r.Players {
		d := kongDelta[i] + winDelta[i]
		pl.Gold += d
		persistGold(pl)
		res = append(res, row{
			Name: pl.Name, Seat: i, Won: st.Won[i], Hand: st.Hands[i],
			KongDelta: kongDelta[i], WinDelta: winDelta[i], Delta: d, Gold: pl.Gold,
		})
	}
	broadcast(r, "game_over", map[string]any{
		"result": res,
		"huang":  st.IsHuang,
		"dealer": st.Dealer,
		"detail": detail,
		"kongs":  st.KongRecords,
	})
	if st.IsHuang {
		r.Dealer = st.Dealer
	} else if st.WinSeat >= 0 {
		r.Dealer = (st.Dealer + 1) % n
	}
	r.InGame = false
	r.State = nil
	broadcast(r, "room", roomView(r))
}

func doRegister(p *Player, raw json.RawMessage) {
	if userStore == nil {
		reply(p.Conn, "error", map[string]string{"msg": "auth unavailable"})
		return
	}
	var d struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Nickname string `json:"nickname"`
	}
	if json.Unmarshal(raw, &d) != nil {
		reply(p.Conn, "error", map[string]string{"msg": "bad register data"})
		return
	}
	u, err := userStore.Register(d.Username, d.Password, d.Nickname)
	if err == auth.ErrUserExists {
		reply(p.Conn, "auth_fail", map[string]any{"msg": "用户名已存在", "action": "register"})
		return
	}
	if err == auth.ErrBadInput {
		reply(p.Conn, "auth_fail", map[string]any{"msg": "用户名3-24位字母数字下划线，密码至少4位", "action": "register"})
		return
	}
	if err != nil {
		reply(p.Conn, "auth_fail", map[string]any{"msg": err.Error(), "action": "register"})
		return
	}
	applyAuthUser(p, u)
	reply(p.Conn, "auth_ok", map[string]any{
		"action":   "register",
		"user_id":  p.UserID,
		"username": p.Username,
		"name":     p.Name,
		"gold":     p.Gold,
		"id":       p.ID,
	})
}

func doLogin(p *Player, raw json.RawMessage) {
	if userStore == nil {
		reply(p.Conn, "error", map[string]string{"msg": "auth unavailable"})
		return
	}
	var d struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.Unmarshal(raw, &d) != nil {
		reply(p.Conn, "error", map[string]string{"msg": "bad login data"})
		return
	}
	u, err := userStore.Login(d.Username, d.Password)
	if err == auth.ErrInvalidLogin {
		reply(p.Conn, "auth_fail", map[string]any{"msg": "用户名或密码错误", "action": "login"})
		return
	}
	if err != nil {
		reply(p.Conn, "auth_fail", map[string]any{"msg": err.Error(), "action": "login"})
		return
	}
	applyAuthUser(p, u)
	reply(p.Conn, "auth_ok", map[string]any{
		"action":   "login",
		"user_id":  p.UserID,
		"username": p.Username,
		"name":     p.Name,
		"gold":     p.Gold,
		"id":       p.ID,
	})
}

func applyAuthUser(p *Player, u *auth.User) {
	p.UserID = u.ID
	p.Username = u.Username
	p.Name = u.Nickname
	p.Gold = u.Gold
	hub.mu.Lock()
	hub.sessions[p.ID] = p
	hub.mu.Unlock()
}

func findRoom(p *Player) *Room {
	for _, r := range hub.rooms {
		for _, x := range r.Players {
			if x.ID == p.ID {
				return r
			}
		}
	}
	return nil
}

func seatOf(r *Room, p *Player) int {
	for i, x := range r.Players {
		if x.ID == p.ID {
			return i
		}
	}
	return -1
}

func names(r *Room) []string {
	out := make([]string, len(r.Players))
	for i, p := range r.Players {
		out[i] = p.Name
	}
	return out
}

func roomView(r *Room) map[string]any {
	ps := make([]map[string]any, 0, len(r.Players))
	for _, p := range r.Players {
		ps = append(ps, map[string]any{"id": p.ID, "name": p.Name, "online": p.Online, "gold": p.Gold})
	}
	return map[string]any{"id": r.ID, "rule": r.Rule, "owner": r.Owner, "in_game": r.InGame, "players": ps}
}

func reply(c *websocket.Conn, typ string, data any) {
	if c == nil {
		return
	}
	b, _ := json.Marshal(data)
	_ = c.WriteJSON(Msg{Type: typ, Data: b})
}

func broadcast(r *Room, typ string, data any) {
	for _, p := range r.Players {
		if p != nil && p.Online && p.Conn != nil {
			reply(p.Conn, typ, data)
		}
	}
}

func id6() string {
	const c = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = c[rand.Intn(len(c))]
	}
	return string(b)
}

func id4() string { return id6()[:4] }
