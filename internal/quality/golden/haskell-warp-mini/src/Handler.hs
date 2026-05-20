module Handler where

import Types (User(..), CreateUserRequest(..), UserStore, HandlerResult)
import Store (getUser, putUser, listUsers)
import Network.Wai (Request, Response, responseLBS, requestBody)
import Network.HTTP.Types (status200, status201, status404, status400)
import Data.Aeson (encode, decode)
import Data.IORef (IORef, readIORef, modifyIORef')
import Control.Monad (when)

-- | Handle GET /users/:id
handleGetUser :: IORef UserStore -> Int -> IO Response
handleGetUser storeRef uid = do
  store <- readIORef storeRef
  case getUser store uid of
    Nothing   -> return $ responseLBS status404 [] "Not Found"
    Just user -> return $ responseLBS status200 [] (encode user)

-- | Handle GET /users
handleListUsers :: IORef UserStore -> IO Response
handleListUsers storeRef = do
  store <- readIORef storeRef
  let users = listUsers store
  return $ responseLBS status200 [] (encode users)

-- | Handle POST /users
handleCreateUser :: IORef UserStore -> Request -> IO Response
handleCreateUser storeRef req = do
  body <- requestBody req
  case decode body of
    Nothing  -> return $ responseLBS status400 [] "Bad Request"
    Just req' -> do
      store <- readIORef storeRef
      let uid  = length (listUsers store) + 1
          user = User uid (createName req') (createEmail req')
      modifyIORef' storeRef (`putUser` user)
      return $ responseLBS status201 [] (encode user)

-- | Validate a user entity
validateUser :: User -> HandlerResult
validateUser user
  | null (userName user)  = Left "name is required"
  | null (userEmail user) = Left "email is required"
  | otherwise             = Right user
