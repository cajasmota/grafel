module Types where

import Data.Map.Strict (Map)

-- | User domain entity
data User = User
  { userId    :: Int
  , userName  :: String
  , userEmail :: String
  } deriving (Show, Eq)

-- | HTTP request body for creating a user
data CreateUserRequest = CreateUserRequest
  { createName  :: String
  , createEmail :: String
  } deriving (Show)

-- | Application configuration
data AppConfig = AppConfig
  { configPort :: Int
  , configHost :: String
  }

-- | In-memory user store type alias
type UserStore = Map Int User

-- | Result type for request handlers
type HandlerResult = Either String User
