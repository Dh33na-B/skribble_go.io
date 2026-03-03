-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- =========================
-- USERS
-- =========================
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(100) UNIQUE,
    password_hash TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

-- =========================
-- ROOMS
-- =========================
CREATE TABLE rooms (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    room_code VARCHAR(10) UNIQUE NOT NULL,
    host_id UUID REFERENCES users(id) ON DELETE SET NULL,
    max_players INT DEFAULT 8,
    status VARCHAR(20) DEFAULT 'waiting', -- waiting, playing, finished
    created_at TIMESTAMP DEFAULT NOW()
);

-- =========================
-- GAMES
-- =========================
CREATE TABLE games (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    room_id UUID REFERENCES rooms(id) ON DELETE CASCADE,
    started_at TIMESTAMP,
    ended_at TIMESTAMP,
    winner_id UUID REFERENCES users(id) ON DELETE SET NULL
);

-- =========================
-- ROUNDS
-- =========================
CREATE TABLE rounds (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    game_id UUID REFERENCES games(id) ON DELETE CASCADE,
    drawer_id UUID REFERENCES users(id) ON DELETE SET NULL,
    word VARCHAR(100) NOT NULL,
    round_number INT NOT NULL,
    started_at TIMESTAMP,
    ended_at TIMESTAMP
);


-- =========================
-- SCORES
-- =========================
CREATE TABLE scores (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    game_id UUID REFERENCES games(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    points INT DEFAULT 0,
    UNIQUE (game_id, user_id)
);

-- =========================
-- WORDS
-- =========================
CREATE TABLE words (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    word VARCHAR(100) UNIQUE NOT NULL,
    difficulty VARCHAR(20) CHECK (difficulty IN ('easy', 'medium', 'hard'))
);

-- =========================
-- INDEXES (Performance)
-- =========================
CREATE INDEX idx_rooms_status ON rooms(status);
CREATE INDEX idx_games_room_id ON games(room_id);
CREATE INDEX idx_rounds_game_id ON rounds(game_id);
CREATE INDEX idx_guesses_round_id ON guesses(round_id);
CREATE INDEX idx_scores_game_id ON scores(game_id);