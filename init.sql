-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- =========================
-- USERS
-- =========================
CREATE TABLE IF NOT EXISTS users (
   id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
   username      VARCHAR(50) UNIQUE NOT NULL,
   email         VARCHAR(100) UNIQUE NOT NULL,
   password_hash TEXT NOT NULL,
   created_at    TIMESTAMP NOT NULL DEFAULT NOW()
);

-- =========================
-- ROOMS
-- =========================
CREATE TABLE IF NOT EXISTS rooms (
   id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
   room_code   VARCHAR(10) UNIQUE NOT NULL,
   host_id     UUID REFERENCES users (id) ON DELETE SET NULL,
   max_players INT NOT NULL DEFAULT 8 CHECK (max_players > 1),
   status      VARCHAR(20) NOT NULL DEFAULT 'waiting' CHECK (status IN ('waiting', 'playing', 'finished')),
   created_at  TIMESTAMP NOT NULL DEFAULT NOW()
);

-- =========================
-- ROOM_PLAYERS
-- =========================
CREATE TABLE IF NOT EXISTS room_players (
   room_id    UUID NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
   user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
   is_host    BOOLEAN NOT NULL DEFAULT FALSE,
   joined_at  TIMESTAMP NOT NULL DEFAULT NOW(),
   PRIMARY KEY (room_id, user_id)
);

-- =========================
-- GAMES
-- =========================
CREATE TABLE IF NOT EXISTS games (
   id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
   room_id    UUID NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
   started_at TIMESTAMP,
   ended_at   TIMESTAMP,
   winner_id  UUID REFERENCES users (id) ON DELETE SET NULL
);

-- =========================
-- ROUNDS
-- =========================
CREATE TABLE IF NOT EXISTS rounds (
   id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
   game_id      UUID NOT NULL REFERENCES games (id) ON DELETE CASCADE,
   drawer_id    UUID REFERENCES users (id) ON DELETE SET NULL,
   word         VARCHAR(100) NOT NULL,
   round_number INT NOT NULL,
   started_at   TIMESTAMP,
   ended_at     TIMESTAMP
);

-- =========================
-- GUESSES
-- =========================
CREATE TABLE IF NOT EXISTS guesses (
   id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
   round_id    UUID NOT NULL REFERENCES rounds (id) ON DELETE CASCADE,
   user_id     UUID REFERENCES users (id) ON DELETE SET NULL,
   guessed_word VARCHAR(100) NOT NULL,
   is_correct  BOOLEAN NOT NULL DEFAULT FALSE,
   guessed_at  TIMESTAMP NOT NULL DEFAULT NOW()
);

-- =========================
-- SCORES
-- =========================
CREATE TABLE IF NOT EXISTS scores (
   id      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
   game_id UUID NOT NULL REFERENCES games (id) ON DELETE CASCADE,
   user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
   points  INT NOT NULL DEFAULT 0,
   UNIQUE (game_id, user_id)
);

-- =========================
-- WORDS
-- =========================
CREATE TABLE IF NOT EXISTS words (
   id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
   word       VARCHAR(100) UNIQUE NOT NULL,
   difficulty VARCHAR(20) CHECK (difficulty IN ('easy', 'medium', 'hard'))
);

-- =========================
-- WORDS (Seed Data)
-- =========================
INSERT INTO words (word, difficulty)
VALUES
   ('apple', 'easy'),
   ('banana', 'easy'),
   ('cat', 'easy'),
   ('dog', 'easy'),
   ('house', 'easy'),
   ('tree', 'easy'),
   ('sun', 'easy'),
   ('moon', 'easy'),
   ('fish', 'easy'),
   ('car', 'easy'),
   ('book', 'easy'),
   ('chair', 'easy'),
   ('phone', 'easy'),
   ('cup', 'easy'),
   ('shoe', 'easy'),
   ('bicycle', 'medium'),
   ('elephant', 'medium'),
   ('guitar', 'medium'),
   ('laptop', 'medium'),
   ('mountain', 'medium'),
   ('rainbow', 'medium'),
   ('robot', 'medium'),
   ('airplane', 'medium'),
   ('camera', 'medium'),
   ('castle', 'medium'),
   ('pizza', 'medium'),
   ('volcano', 'medium'),
   ('spider', 'medium'),
   ('dolphin', 'medium'),
   ('bridge', 'medium'),
   ('binoculars', 'hard'),
   ('compass', 'hard'),
   ('chandelier', 'hard'),
   ('parachute', 'hard'),
   ('skyscraper', 'hard'),
   ('submarine', 'hard'),
   ('microscope', 'hard'),
   ('helicopter', 'hard'),
   ('astronaut', 'hard'),
   ('kangaroo', 'hard'),
   ('lighthouse', 'hard'),
   ('saxophone', 'hard'),
   ('carousel', 'hard'),
   ('thermometer', 'hard'),
   ('windmill', 'hard')
ON CONFLICT (word) DO NOTHING;

-- =========================
-- INDEXES (Performance)
-- =========================
CREATE INDEX IF NOT EXISTS idx_rooms_status ON rooms (status);
CREATE INDEX IF NOT EXISTS idx_games_room_id ON games (room_id);
CREATE INDEX IF NOT EXISTS idx_rounds_game_id ON rounds (game_id);
CREATE INDEX IF NOT EXISTS idx_guesses_round_id ON guesses (round_id);
CREATE INDEX IF NOT EXISTS idx_scores_game_id ON scores (game_id);
CREATE INDEX IF NOT EXISTS idx_room_players_room_id ON room_players (room_id);
