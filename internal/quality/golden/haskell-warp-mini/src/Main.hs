module Main where

import Types (AppConfig(..), UserStore)
import Store (newStore)
import Handler (handleGetUser, handleListUsers, handleCreateUser)
import Network.Wai (Application, Request, Response)
import Network.Wai.Handler.Warp (run, runSettings, defaultSettings, setPort, setHost)
import Network.HTTP.Types (status404)
import Network.Wai (responseLBS)
import Data.IORef (newIORef)
import System.Environment (getArgs)
import Control.Exception (catch, SomeException)

-- | Route the request to the appropriate handler
router :: IORef UserStore -> Application
router storeRef req respond = do
  -- Simple path-based routing
  let path = pathInfo req
  response <- dispatchRequest storeRef req path
  respond response

-- | Dispatch based on request path segments
dispatchRequest :: IORef UserStore -> Request -> [Text] -> IO Response
dispatchRequest storeRef req path = case path of
  ["users"]     -> handleListUsers storeRef
  ["users", _n] -> handleGetUser storeRef 1
  _             -> return $ responseLBS status404 [] "Not Found"

-- | Build the WAI application
mkApp :: IORef UserStore -> Application
mkApp = router

-- | Load configuration from environment args
loadConfig :: IO AppConfig
loadConfig = do
  args <- getArgs
  let port = case args of
               (p:_) -> read p
               []    -> 8080
  return $ AppConfig port "localhost"

-- | Application entry point
main :: IO ()
main = do
  cfg <- loadConfig
  storeRef <- newIORef newStore
  let port = configPort cfg
  putStrLn ("Listening on port " ++ show port)
  let settings = setPort port $ setHost "localhost" defaultSettings
  catch (runSettings settings (mkApp storeRef)) handler
  where
    handler :: SomeException -> IO ()
    handler ex = putStrLn ("Server error: " ++ show ex)
