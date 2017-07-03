
-- +migrate Up
CREATE TABLE team (
    city VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    player1 VARCHAR(255) NOT NULL,
    player2 VARCHAR(255) NOT NULL,
    wins INTEGER NOT NULL DEFAULT 0,
    losses INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE game (
    id SERIAL PRIMARY KEY,
    timestamp BIGINT NOT NULL DEFAULT EXTRACT(epoch FROM NOW()) * 1000,
    black_team VARCHAR(255) NOT NULL,
    yellow_team VARCHAR(255) NOT NULL,
    black_team_player1_goals VARCHAR(255) NOT NULL,
    black_team_player2_goals VARCHAR(255) NOT NULL,
    yellow_team_player1_goals VARCHAR(255) NOT NULL,
    yellow_team_player2_goals VARCHAR(255) NOT NULL
);

CREATE TABLE player (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    picture TEXT,
    wins INTEGER NOT NULL DEFAULT 0,
    losses INTEGER NOT NULL DEFAULT 0,
    goals INTEGER NOT NULL DEFAULT 0
);

-- +migrate Down
DROP TABLE player;
DROP TABLE game;
DROP TABLE team;
