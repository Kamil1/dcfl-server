
-- +migrate Up
CREATE TABLE team (
    id SERIAL PRIMARY KEY,
    city VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    player1 VARCHAR(255) NOT NULL,
    player2 VARCHAR(255) NOT NULL
);

CREATE TABLE game (
    id SERIAL PRIMARY KEY,
    start_timestamp BIGINT NOT NULL DEFAULT EXTRACT(epoch FROM NOW()) * 1000,
    end_timestamp BIGINT,
    black_team INTEGER NOT NULL,
    yellow_team INTEGER NOT NULL,
    black_score INTEGER,
    yellow_score INTEGER
);

CREATE TABLE game_goals (
    game_id INTEGER NOT NULL,
    player_id VARCHAR(255) NOT NULL,
    goals INTEGER NOT NULL
);

CREATE TABLE player (
    id VARCHAR(255) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    picture TEXT
);

-- +migrate Down
DROP TABLE player;
DROP TABLE game_goals;
DROP TABLE game;
DROP TABLE team;
