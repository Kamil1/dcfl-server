package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type hub struct {
	// Connections mutex.
	connectionsMx sync.RWMutex

	// Registered connections.
	connections map[*connection]struct{}

	// Side mutex.
	sideMx sync.RWMutex

	// Registered black players.
	blackSide [2]player

	blackTeam team

	// Registered yellow players.
	yellowSide [2]player

	yellowTeam team

	// Score mutex.
	scoreMx sync.RWMutex

	blackScore int

	yellowScore int

	gameStarted bool

	gameOver bool

	gameID int

	// Inbound request messages from the connections.
	requests chan []byte

	// Outbound messages from the server.
	confirmations chan string
}

type dcflMsg struct {
	// the action performed by the server
	Action string `json:"action"`
	// the id of the user the action was performed against, if applicable
	Sub string `json:"sub"`
	// the side the action was performed against, if applicable
	Side string `json:"side"`
	// picture related to the action, if applicable
	Picture string `json:"picture"`
	// player1, if applicable
	Player1 string `json:"player_1"`
	// player2, if applicable
	Player2 string `json:"player_2"`
	// city, if applicable
	City string `json:"city"`
	// name, if applicable
	Name string `json:"name"`
}

type matchState struct {
	BlackPlayer1  player `json:"black_player_1"`
	BlackPlayer2  player `json:"black_player_2"`
	YellowPlayer1 player `json:"yellow_player_1"`
	YellowPlayer2 player `json:"yellow_player_2"`
	BlackTeam     team   `json:"black_team"`
	YellowTeam    team   `json:"yellow_team"`
	BlackScore    int    `json:"black_score"`
	YellowScore   int    `json:"yellow_score"`
	GameStarted   bool   `json:"game_started"`
	GameOver      bool   `json:"game_over"`
	Error         string `json:"error"`
}

type player struct {
	Sub       string `json:"sub"`
	Picture   string `json:"picture"`
	Confirmed bool   `json:"confirmed"`
	Goals     int    `json:"goals"`
}

type team struct {
	ID   int    `json:"id"`
	City string `json:"city"`
	Name string `json:"name"`
}

func startGame(h *hub) error {
	var id int
	err := db.QueryRow(
		"INSERT INTO public.game(black_team, yellow_team) VALUES ($1, $2) RETURNING id",
		h.blackTeam.ID,
		h.yellowTeam.ID).Scan(&id)
	if err != nil {
		return err
	}
	h.gameID = id
	h.gameStarted = true
	return nil
}

func endGame(h *hub) {
	h.gameOver = true
	_, err := db.Exec(
		"UPDATE public.game SET end_timestamp = EXTRACT(epoch FROM NOW()) * 1000, black_score = $1, yellow_score = $2 WHERE id = $3",
		h.blackScore,
		h.yellowScore,
		h.gameID)
	if err != nil {
		fmt.Println("Error recording game end_timestamp")
		fmt.Println(err)
	}
	for _, p := range h.blackSide {
		_, err := db.Exec(
			"INSERT INTO public.game_goals(game_id, player_id, goals) VALUES ($1, $2, $3)",
			h.gameID,
			p.Sub,
			p.Goals)
		if err != nil {
			fmt.Println("Error recording black player goals")
			fmt.Println(err)
		}
	}
	for _, p := range h.yellowSide {
		_, err := db.Exec(
			"INSERT INTO public.game_goals(game_id, player_id, goals) VALUES ($1, $2, $3)",
			h.gameID,
			p.Sub,
			p.Goals)
		if err != nil {
			fmt.Println("Error recording yellow player goals")
			fmt.Println(err)
		}
	}
}

// This function assumes and requires the caller to have the sideMx and scoreMx locks acquired.
func reset(h *hub, resetReason string) {
	h.blackTeam = team{}
	h.yellowTeam = team{}
	h.blackSide[0] = player{}
	h.blackSide[1] = player{}
	h.yellowSide[0] = player{}
	h.yellowSide[1] = player{}

	h.blackScore = 0
	h.yellowScore = 0
	h.gameStarted = false
	h.gameOver = false
	fmt.Println("done removing conns")
}

// Assumes and requires that caller has acquired sideMx lock.
func registerGame(h *hub, cm *dcflMsg) {
	fmt.Println("Registering")
	if cm.Side == "black" {
		// Check if already registered on black side.
		found := false
		confirmed := false
		for _, v := range h.blackSide {
			if v.Sub == cm.Sub {
				found = true
				confirmed = v.Confirmed
			}
		}
		if found {
			if !confirmed {
				fmt.Println("Already registered to black side, unregistering")
				unregisterGame(h, cm)
			}
			fmt.Println("Already registered and confirmed to black side")
			return
		}

		// Check if black side is full.
		if h.blackSide[0] != (player{}) && h.blackSide[1] != (player{}) {
			fmt.Println("Black side full")
			return
		}
	} else if cm.Side == "yellow" {
		// Check if already registered on yellow side.
		found := false
		confirmed := false
		for _, v := range h.yellowSide {
			if v.Sub == cm.Sub {
				found = true
				confirmed = v.Confirmed
			}
		}
		if found {
			if !confirmed {
				fmt.Println("Already registered to yellow side, unregistering")
				unregisterGame(h, cm)
			}
			fmt.Println("Already registered and confirmed to yellow side")
			return
		}

		// Check if yellow side is full.
		if h.yellowSide[0] != (player{}) && h.yellowSide[1] != (player{}) {
			fmt.Println("Yellow side full")
			return
		}
	} else {
		// If side is neither black nor yellow, ignore the request.
		return
	}

	fmt.Println("Getting user picture")
	var picture string
	err := db.QueryRow("SELECT picture FROM public.player WHERE id = $1", cm.Sub).Scan(&picture)
	if err != nil {
		return
	}

	if cm.Side == "black" {
		// If registering for black side, unregister from yellow side.
		fmt.Println("Registering to black side")
		var err error
		for _, v := range h.yellowSide {
			if v.Sub == cm.Sub {
				fmt.Println("Unregistering from yellow side")
				unregisterMsg := cm
				unregisterMsg.Side = "yellow"
				unregisterGame(h, unregisterMsg)
			}
		}
		if err != nil {
			return
		}

		// Register to first free side slot.
		fmt.Println("Placing in black player slot")
		if h.blackSide[0] == (player{}) {
			fmt.Println("Placed in black slot 1")
			h.blackSide[0] = player{Sub: cm.Sub, Picture: picture, Confirmed: false}
		} else if h.blackSide[1] == (player{}) {
			fmt.Println("Placed in black slot 2")
			h.blackSide[1] = player{Sub: cm.Sub, Picture: picture, Confirmed: false}
		}
	} else {
		// If registering for yellow side, unregister from black side.
		fmt.Println("Registering to yellow side")
		var err error
		for _, v := range h.blackSide {
			if v.Sub == cm.Sub {
				fmt.Println("Unregistering from black side")
				unregisterMsg := cm
				unregisterMsg.Side = "black"
				unregisterGame(h, unregisterMsg)
			}
		}
		if err != nil {
			return
		}

		// Register to first free side slot.
		fmt.Println("Placing in yellow player slot")
		if h.yellowSide[0] == (player{}) {
			fmt.Println("Placed in yellow slot 1")
			h.yellowSide[0] = player{Sub: cm.Sub, Picture: picture, Confirmed: false}
		} else if h.yellowSide[1] == (player{}) {
			fmt.Println("Placed in yellow slot 2")
			h.yellowSide[1] = player{Sub: cm.Sub, Picture: picture, Confirmed: false}
		}
	}
}

// This function assumes and requires the sideMx lock to be acquired by the caller.
func unregisterGame(h *hub, cm *dcflMsg) {
	fmt.Println("Unregistering")
	if cm.Side == "black" {
		if h.blackSide[0].Sub == cm.Sub {
			h.blackSide[0] = player{}
		} else if h.blackSide[1].Sub == cm.Sub {
			h.blackSide[1] = player{}
		}
	} else if cm.Side == "yellow" {
		if h.yellowSide[0].Sub == cm.Sub {
			h.yellowSide[0] = player{}
		} else if h.yellowSide[1].Sub == cm.Sub {
			h.yellowSide[1] = player{}
		}
	} else {
		return
	}

	fmt.Println("Completed unregistration")
}

// This function assumes and requires the sideMx lock to be acquired by the caller.
func confirmPlayer(h *hub, cm *dcflMsg) {
	fmt.Println("Confirming")
	if cm.Side == "black" {
		if h.blackSide[0].Sub == cm.Sub {
			fmt.Println("Confirming black player 1")
			h.blackSide[0].Confirmed = true
		} else if h.blackSide[1].Sub == cm.Sub {
			fmt.Println("Confirming black player 2")
			h.blackSide[1].Confirmed = true
		} else {
			fmt.Println("Black player not found")
			return
		}

		// Get team name.
		if h.blackSide[0].Confirmed && h.blackSide[1].Confirmed {
			team, err := getTeam(h.blackSide[0].Sub, h.blackSide[1].Sub)
			if err != nil {
				fmt.Println(err)
				return
			}
			h.blackTeam = team
		}

	} else if cm.Side == "yellow" {
		if h.yellowSide[0].Sub == cm.Sub {
			fmt.Println("Confirming yellow player 1")
			h.yellowSide[0].Confirmed = true
		} else if h.yellowSide[1].Sub == cm.Sub {
			fmt.Println("Confirming yellow player 2")
			h.yellowSide[1].Confirmed = true
		} else {
			fmt.Println("Yellow player not found")
			return
		}

		// Get team name.
		if h.yellowSide[0].Confirmed && h.yellowSide[1].Confirmed {
			team, err := getTeam(h.yellowSide[0].Sub, h.yellowSide[1].Sub)
			if err != nil {
				fmt.Println(err)
				return
			}
			h.yellowTeam = team
		}
	}

	if h.blackSide[0].Confirmed &&
		h.blackSide[1].Confirmed &&
		h.yellowSide[0].Confirmed &&
		h.yellowSide[1].Confirmed &&
		h.blackTeam != (team{}) &&
		h.yellowTeam != (team{}) {
		err := startGame(h)
		if err != nil {
			h.scoreMx.Lock()
			defer h.scoreMx.Unlock()
			reset(h, "Error starting game")
			return
		}
	}
}

// This function assumes and requires the sideMx lock to be acquired by the caller.
func registerTeam(h *hub, cm *dcflMsg) {
	fmt.Println("Registering team")
	if cm.Player1 == "" ||
		cm.Player2 == "" ||
		cm.Player1 == cm.Player2 ||
		cm.City == "" ||
		cm.Name == "" {
		h.scoreMx.Lock()
		defer h.scoreMx.Unlock()
		reset(h, "Error registering teams")
		return
	}
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM public.team WHERE (player1 = $1 AND player2 = $2) OR (player1 = $2 AND player2 = $1) OR city = $3 OR name = $4",
		cm.Player1,
		cm.Player2,
		cm.City,
		cm.Name).Scan(&count)
	if err != nil || count != 0 {
		h.scoreMx.Lock()
		defer h.scoreMx.Unlock()
		reset(h, "Error registering teams")
		return
	}

	var id int
	err = db.QueryRow(
		"INSERT INTO public.team(city, name, player1, player2) VALUES ($1, $2, $3, $4) RETURNING id",
		cm.City,
		cm.Name,
		cm.Player1,
		cm.Player2).Scan(&id)
	if err != nil {
		h.scoreMx.Lock()
		defer h.scoreMx.Unlock()
		reset(h, "Error creating team")
		return
	}

	teamObj := team{ID: id, City: cm.City, Name: cm.Name}

	if cm.Side == "black" {
		h.blackTeam = teamObj
	} else if cm.Side == "yellow" {
		h.yellowTeam = teamObj
	} else {
		h.scoreMx.Lock()
		defer h.scoreMx.Unlock()
		reset(h, "Error setting up team")
		return
	}

	if h.blackSide[0].Confirmed &&
		h.blackSide[1].Confirmed &&
		h.yellowSide[0].Confirmed &&
		h.yellowSide[1].Confirmed &&
		h.blackTeam != (team{}) &&
		h.yellowTeam != (team{}) {
		err := startGame(h)
		if err != nil {
			h.scoreMx.Lock()
			defer h.scoreMx.Unlock()
			reset(h, "Error starting game")
			return
		}
	}
}

// This function assumes and requires the scoreMx and sideMx locks to be acquired by the caller.
func registerGoal(h *hub, cm *dcflMsg) {
	fmt.Println("Registering goal")
	// Must be in game to score goal.
	if !h.gameStarted || h.gameOver {
		return
	}
	if h.blackSide[0].Sub == cm.Sub {
		h.blackSide[0].Goals++
		h.blackScore++
		if h.blackScore == 5 || h.yellowScore == 5 {
			endGame(h)
			reset(h, "Game Over")
			return
		}
	} else if h.blackSide[1].Sub == cm.Sub {
		h.blackSide[1].Goals++
		h.blackScore++
		if h.blackScore == 5 || h.yellowScore == 5 {
			endGame(h)
			reset(h, "Game Over")
			return
		}
	} else if h.yellowSide[0].Sub == cm.Sub {
		h.yellowSide[0].Goals++
		h.yellowScore++
		if h.blackScore == 5 || h.yellowScore == 5 {
			endGame(h)
			reset(h, "Game Over")
			return
		}
	} else if h.yellowSide[1].Sub == cm.Sub {
		h.yellowSide[1].Goals++
		h.yellowScore++
		if h.blackScore == 5 || h.yellowScore == 5 {
			endGame(h)
			reset(h, "Game Over")
			return
		}
	} else {
		// Player not in game.
		return
	}
	fmt.Println("Unlocking mutexes in goal")
	fmt.Println("Done goal")
}

// This function assumes and requires the caller to have the sideMx and scoreMx locks acquired.
func unregisterGoal(h *hub, cm *dcflMsg) {
	fmt.Println("Undoing goal")
	// Must be in game to undo goal.
	if !h.gameStarted || h.gameOver {
		return
	}
	if h.blackSide[0].Sub == cm.Sub {
		h.blackSide[0].Goals--
		h.blackScore--
	} else if h.blackSide[1].Sub == cm.Sub {
		h.blackSide[1].Goals--
		h.blackScore--
	} else if h.yellowSide[0].Sub == cm.Sub {
		h.yellowSide[0].Goals--
		h.yellowScore--
	} else if h.yellowSide[1].Sub == cm.Sub {
		h.yellowSide[1].Goals--
		h.yellowScore--
	} else {
		// Player not in game.
		return
	}
}

func getTeam(player1 string, player2 string) (team, error) {
	var id int
	var city string
	var name string
	err := db.QueryRow(
		"SELECT id, city, name FROM public.team WHERE (player1 = $1 AND player2 = $2) OR (player1 = $2 AND player2 = $1)",
		player1,
		player2,
	).Scan(&id, &city, &name)
	if err == sql.ErrNoRows {
		// If client sees that both players have confirmed, but team is empty,
		// it must prompt user to register team.
		return team{}, nil
	} else if err != nil {
		return team{}, err
	}

	return team{ID: id, City: city, Name: name}, nil
}

func newHub() *hub {
	h := &hub{
		connectionsMx: sync.RWMutex{},
		requests:      make(chan []byte, 1),
		confirmations: make(chan string),
		sideMx:        sync.RWMutex{},
		blackTeam:     team{},
		yellowTeam:    team{},
		scoreMx:       sync.RWMutex{},
		blackScore:    0,
		yellowScore:   0,
		gameStarted:   false,
		gameOver:      false,
		connections:   make(map[*connection]struct{}),
	}

	go func() {
		for {
			fmt.Println("polling...")
			msg := <-h.requests

			cm := &dcflMsg{}
			err := json.Unmarshal(msg, cm)
			if err != nil {
				continue
			}

			switch cm.Action {
			case "register game":
				h.sideMx.Lock()
				registerGame(h, cm)
				h.sideMx.Unlock()
			case "unregister":
				h.sideMx.Lock()
				unregisterGame(h, cm)
				h.sideMx.Unlock()
			case "confirm":
				h.sideMx.Lock()
				confirmPlayer(h, cm)
				h.sideMx.Unlock()
			case "register team":
				h.sideMx.Lock()
				registerTeam(h, cm)
				h.sideMx.Unlock()
			case "goal":
				h.scoreMx.Lock()
				h.sideMx.Lock()
				registerGoal(h, cm)
				h.sideMx.Unlock()
				h.scoreMx.Unlock()
			case "undo goal":
				h.scoreMx.Lock()
				h.sideMx.Lock()
				unregisterGoal(h, cm)
				h.sideMx.Unlock()
				h.scoreMx.Unlock()
			}
			h.confirmations <- "match state"
		}
	}()

	go func() {
		for {
			var msg []byte
			confirmation := <-h.confirmations
			fmt.Println("Sending confirmations")

			h.sideMx.RLock()
			h.connectionsMx.Lock()
			switch confirmation {
			case "match state":
				fmt.Println("Case 1")
				state := matchState{
					BlackPlayer1:  h.blackSide[0],
					BlackPlayer2:  h.blackSide[1],
					YellowPlayer1: h.yellowSide[0],
					YellowPlayer2: h.yellowSide[1],
					BlackTeam:     h.blackTeam,
					YellowTeam:    h.yellowTeam,
					BlackScore:    h.blackScore,
					YellowScore:   h.yellowScore,
					GameStarted:   h.gameStarted,
					GameOver:      h.gameOver,
				}
				stateJSON, _ := json.Marshal(state)
				msg = stateJSON
			default:
				fmt.Println("Case 2")
				state := matchState{Error: confirmation}
				stateJSON, _ := json.Marshal(state)
				msg = stateJSON
			}
			for c := range h.connections {
				select {
				case c.send <- msg:
				// stop trying to send to this connection after trying for 1 second.
				// if we have to stop, it means that a reader died so remove the connection also.
				case <-time.After(1 * time.Second):
					h.removeConnection(c)
				}
			}
			h.sideMx.RUnlock()
			h.connectionsMx.Unlock()
			fmt.Println("Confirmations sent successfully.")
		}
	}()

	return h
}

func (h *hub) addConnection(conn *connection) {
	h.connectionsMx.Lock()
	defer h.connectionsMx.Unlock()
	h.connections[conn] = struct{}{}
	h.confirmations <- "match state"
}

// This function assumes and requires both the sideMx and connectionsMx locks to be
// acquired by the caller.
func (h *hub) removeConnection(conn *connection) {
	fmt.Println("remove conn called")
	fmt.Println("removing...")
	if _, ok := h.connections[conn]; ok {
		blackMsg := dcflMsg{Sub: conn.sub, Side: "black"}
		yellowMsg := dcflMsg{Sub: conn.sub, Side: "yellow"}
		unregisterGame(h, &blackMsg)
		unregisterGame(h, &yellowMsg)
		delete(h.connections, conn)
		close(conn.send)
	}
}
