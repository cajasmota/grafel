module Store where

import Types (User(..), UserStore)
import qualified Data.Map.Strict as Map
import Data.Maybe (fromMaybe)

-- | Create an empty user store
newStore :: UserStore
newStore = Map.empty

-- | Look up a user by numeric ID
getUser :: UserStore -> Int -> Maybe User
getUser store uid = Map.lookup uid store

-- | Insert or update a user in the store
putUser :: UserStore -> User -> UserStore
putUser store user = Map.insert (userId user) user store

-- | Delete a user from the store
deleteUser :: UserStore -> Int -> UserStore
deleteUser store uid = Map.delete uid store

-- | List all users in the store
listUsers :: UserStore -> [User]
listUsers = Map.elems

-- | Count the number of users
countUsers :: UserStore -> Int
countUsers = Map.size
