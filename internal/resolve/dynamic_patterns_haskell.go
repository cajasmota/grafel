package resolve

import "regexp"

// haskellDynamicPatterns are per-language patterns for Haskell.
// Registered via init() into dynamicPatternsByLang.
//
// The Haskell extractor (internal/extractors/haskell/extractor.go) emits
// CALLS edges whose ToID is the bare function name at the call site.
//
// Three categories of patterns:
//
//  1. Haskell Prelude — functions in the implicit Prelude import.
//     These are distinctive enough to identify as Haskell with high confidence.
//
//  2. Monad/Applicative combinators — functional operators emitted as
//     identifiers (>>=, >>, <$>, <*>, pure, return are gated to lang=="haskell"
//     since "return" appears in many languages but means IO in Haskell).
//
//  3. Common library functions — Data.Map, Data.List, Control.Monad, Text.Printf.
//     Gated to lang=="haskell" to avoid collision.
//
// All patterns are gated to lang=="haskell" (safer-bias rule).
var haskellDynamicPatterns = []*regexp.Regexp{
	// ── Prelude ──────────────────────────────────────────────────────
	regexp.MustCompile(`^map$`),          // Prelude.map :: (a -> b) -> [a] -> [b]
	regexp.MustCompile(`^filter$`),       // Prelude.filter :: (a -> Bool) -> [a] -> [a]
	regexp.MustCompile(`^foldr$`),        // Prelude.foldr :: (a -> b -> b) -> b -> [a] -> b
	regexp.MustCompile(`^foldl$`),        // Prelude.foldl :: (b -> a -> b) -> b -> [a] -> b
	regexp.MustCompile(`^foldl'$`),       // Data.List.foldl' (strict variant)
	regexp.MustCompile(`^foldr1$`),       // Prelude.foldr1 :: (a -> a -> a) -> [a] -> a
	regexp.MustCompile(`^foldl1$`),       // Prelude.foldl1 :: (a -> a -> a) -> [a] -> a
	regexp.MustCompile(`^show$`),         // Prelude.show :: Show a => a -> String
	regexp.MustCompile(`^read$`),         // Prelude.read :: Read a => String -> a
	regexp.MustCompile(`^print$`),        // Prelude.print :: Show a => a -> IO ()
	regexp.MustCompile(`^putStrLn$`),     // Prelude.putStrLn :: String -> IO ()
	regexp.MustCompile(`^putStr$`),       // Prelude.putStr :: String -> IO ()
	regexp.MustCompile(`^getLine$`),      // Prelude.getLine :: IO String
	regexp.MustCompile(`^length$`),       // Prelude.length :: [a] -> Int
	regexp.MustCompile(`^head$`),         // Prelude.head :: [a] -> a
	regexp.MustCompile(`^tail$`),         // Prelude.tail :: [a] -> [a]
	regexp.MustCompile(`^last$`),         // Prelude.last :: [a] -> a
	regexp.MustCompile(`^init$`),         // Prelude.init :: [a] -> [a]
	regexp.MustCompile(`^null$`),         // Prelude.null :: [a] -> Bool
	regexp.MustCompile(`^reverse$`),      // Prelude.reverse :: [a] -> [a]
	regexp.MustCompile(`^concat$`),       // Prelude.concat :: [[a]] -> [a]
	regexp.MustCompile(`^concatMap$`),    // Prelude.concatMap :: (a -> [b]) -> [a] -> [b]
	regexp.MustCompile(`^zip$`),          // Prelude.zip :: [a] -> [b] -> [(a, b)]
	regexp.MustCompile(`^zipWith$`),      // Prelude.zipWith :: (a -> b -> c) -> [a] -> [b] -> [c]
	regexp.MustCompile(`^unzip$`),        // Prelude.unzip :: [(a, b)] -> ([a], [b])
	regexp.MustCompile(`^iterate$`),      // Prelude.iterate :: (a -> a) -> a -> [a]
	regexp.MustCompile(`^replicate$`),    // Prelude.replicate :: Int -> a -> [a]
	regexp.MustCompile(`^take$`),         // Prelude.take :: Int -> [a] -> [a]
	regexp.MustCompile(`^drop$`),         // Prelude.drop :: Int -> [a] -> [a]
	regexp.MustCompile(`^takeWhile$`),    // Prelude.takeWhile :: (a -> Bool) -> [a] -> [a]
	regexp.MustCompile(`^dropWhile$`),    // Prelude.dropWhile :: (a -> Bool) -> [a] -> [a]
	regexp.MustCompile(`^span$`),         // Prelude.span :: (a -> Bool) -> [a] -> ([a], [a])
	regexp.MustCompile(`^break$`),        // Prelude.break :: (a -> Bool) -> [a] -> ([a], [a])
	regexp.MustCompile(`^elem$`),         // Prelude.elem :: Eq a => a -> [a] -> Bool
	regexp.MustCompile(`^notElem$`),      // Prelude.notElem :: Eq a => a -> [a] -> Bool
	regexp.MustCompile(`^lookup$`),       // Prelude.lookup :: Eq a => a -> [(a, b)] -> Maybe b
	regexp.MustCompile(`^maximum$`),      // Prelude.maximum :: Ord a => [a] -> a
	regexp.MustCompile(`^minimum$`),      // Prelude.minimum :: Ord a => [a] -> a
	regexp.MustCompile(`^sum$`),          // Prelude.sum :: Num a => [a] -> a
	regexp.MustCompile(`^product$`),      // Prelude.product :: Num a => [a] -> a
	regexp.MustCompile(`^and$`),          // Prelude.and :: [Bool] -> Bool
	regexp.MustCompile(`^or$`),           // Prelude.or :: [Bool] -> Bool
	regexp.MustCompile(`^any$`),          // Prelude.any :: (a -> Bool) -> [a] -> Bool
	regexp.MustCompile(`^all$`),          // Prelude.all :: (a -> Bool) -> [a] -> Bool
	regexp.MustCompile(`^words$`),        // Prelude.words :: String -> [String]
	regexp.MustCompile(`^unwords$`),      // Prelude.unwords :: [String] -> String
	regexp.MustCompile(`^lines$`),        // Prelude.lines :: String -> [String]
	regexp.MustCompile(`^unlines$`),      // Prelude.unlines :: [String] -> String
	regexp.MustCompile(`^interact$`),     // Prelude.interact :: (String -> String) -> IO ()
	regexp.MustCompile(`^readFile$`),     // Prelude.readFile :: FilePath -> IO String
	regexp.MustCompile(`^writeFile$`),    // Prelude.writeFile :: FilePath -> String -> IO ()
	regexp.MustCompile(`^appendFile$`),   // Prelude.appendFile :: FilePath -> String -> IO ()
	regexp.MustCompile(`^error$`),        // Prelude.error :: String -> a (runtime error)
	regexp.MustCompile(`^undefined$`),    // Prelude.undefined :: a (intentional partial)
	regexp.MustCompile(`^seq$`),          // Prelude.seq :: a -> b -> b (strict eval)
	regexp.MustCompile(`^id$`),           // Prelude.id :: a -> a
	regexp.MustCompile(`^const$`),        // Prelude.const :: a -> b -> a
	regexp.MustCompile(`^flip$`),         // Prelude.flip :: (a -> b -> c) -> b -> a -> c
	regexp.MustCompile(`^curry$`),        // Prelude.curry :: ((a, b) -> c) -> a -> b -> c
	regexp.MustCompile(`^uncurry$`),      // Prelude.uncurry :: (a -> b -> c) -> (a, b) -> c
	regexp.MustCompile(`^fst$`),          // Prelude.fst :: (a, b) -> a
	regexp.MustCompile(`^snd$`),          // Prelude.snd :: (a, b) -> b
	regexp.MustCompile(`^not$`),          // Prelude.not :: Bool -> Bool
	regexp.MustCompile(`^even$`),         // Prelude.even :: Integral a => a -> Bool
	regexp.MustCompile(`^odd$`),          // Prelude.odd :: Integral a => a -> Bool
	regexp.MustCompile(`^abs$`),          // Prelude.abs :: Num a => a -> a
	regexp.MustCompile(`^signum$`),       // Prelude.signum :: Num a => a -> a
	regexp.MustCompile(`^negate$`),       // Prelude.negate :: Num a => a -> a
	regexp.MustCompile(`^fromIntegral$`), // Prelude.fromIntegral :: (Integral a, Num b) => a -> b
	regexp.MustCompile(`^toInteger$`),    // Prelude.toInteger :: Integral a => a -> Integer
	regexp.MustCompile(`^floor$`),        // Prelude.floor :: (RealFrac a, Integral b) => a -> b
	regexp.MustCompile(`^ceiling$`),      // Prelude.ceiling :: (RealFrac a, Integral b) => a -> b
	regexp.MustCompile(`^round$`),        // Prelude.round :: (RealFrac a, Integral b) => a -> b
	regexp.MustCompile(`^truncate$`),     // Prelude.truncate :: (RealFrac a, Integral b) => a -> b
	regexp.MustCompile(`^sqrt$`),         // Prelude.sqrt :: Floating a => a -> a
	regexp.MustCompile(`^pi$`),           // Prelude.pi :: Floating a => a

	// ── Monad / Applicative combinators ──────────────────────────────
	// These are identifier names (not operators) that the extractor may emit.
	regexp.MustCompile(`^pure$`),      // Applicative.pure :: a -> f a
	regexp.MustCompile(`^return$`),    // Monad.return :: a -> m a (gated to haskell)
	regexp.MustCompile(`^mapM$`),      // Control.Monad.mapM :: Monad m => (a -> m b) -> [a] -> m [b]
	regexp.MustCompile(`^mapM_$`),     // Control.Monad.mapM_ :: Monad m => (a -> m b) -> [a] -> m ()
	regexp.MustCompile(`^forM$`),      // Control.Monad.forM :: Monad m => [a] -> (a -> m b) -> m [b]
	regexp.MustCompile(`^forM_$`),     // Control.Monad.forM_ :: Monad m => [a] -> (a -> m b) -> m ()
	regexp.MustCompile(`^sequence$`),  // Monad.sequence :: Monad m => [m a] -> m [a]
	regexp.MustCompile(`^sequence_$`), // Monad.sequence_ :: Monad m => [m a] -> m ()
	regexp.MustCompile(`^void$`),      // Control.Monad.void :: Functor f => f a -> f ()
	regexp.MustCompile(`^when$`),      // Control.Monad.when :: Monad m => Bool -> m () -> m ()
	regexp.MustCompile(`^unless$`),    // Control.Monad.unless :: Monad m => Bool -> m () -> m ()
	regexp.MustCompile(`^guard$`),     // Control.Monad.guard :: MonadPlus m => Bool -> m ()
	regexp.MustCompile(`^join$`),      // Control.Monad.join :: Monad m => m (m a) -> m a
	regexp.MustCompile(`^liftIO$`),    // Control.Monad.IO.Class.liftIO :: MonadIO m => IO a -> m a
	regexp.MustCompile(`^liftM$`),     // Control.Monad.liftM :: Monad m => (a -> b) -> m a -> m b
	regexp.MustCompile(`^liftM2$`),    // Control.Monad.liftM2 :: Monad m => (a -> b -> c) -> m a -> m b -> m c
	regexp.MustCompile(`^fmap$`),      // Functor.fmap :: Functor f => (a -> b) -> f a -> f b
	regexp.MustCompile(`^mconcat$`),   // Monoid.mconcat :: [a] -> a
	regexp.MustCompile(`^mempty$`),    // Monoid.mempty :: a
	regexp.MustCompile(`^mappend$`),   // Monoid.mappend :: a -> a -> a

	// ── Data.Map ──────────────────────────────────────────────────────
	// These are distinctive enough to gate to haskell safely.
	regexp.MustCompile(`^fromList$`),         // Data.Map.fromList :: Ord k => [(k, v)] -> Map k v
	regexp.MustCompile(`^toList$`),           // Data.Map.toList :: Map k v -> [(k, v)]
	regexp.MustCompile(`^toAscList$`),        // Data.Map.toAscList :: Map k v -> [(k, v)]
	regexp.MustCompile(`^insertWith$`),       // Data.Map.insertWith :: Ord k => (a -> a -> a) -> k -> a -> Map k a -> Map k a
	regexp.MustCompile(`^unionWith$`),        // Data.Map.unionWith :: Ord k => (a -> a -> a) -> Map k a -> Map k a -> Map k a
	regexp.MustCompile(`^intersectionWith$`), // Data.Map.intersectionWith
	regexp.MustCompile(`^differenceWith$`),   // Data.Map.differenceWith
	regexp.MustCompile(`^adjust$`),           // Data.Map.adjust :: Ord k => (a -> a) -> k -> Map k a -> Map k a
	regexp.MustCompile(`^findWithDefault$`),  // Data.Map.findWithDefault
	regexp.MustCompile(`^mapWithKey$`),       // Data.Map.mapWithKey
	regexp.MustCompile(`^foldlWithKey$`),     // Data.Map.foldlWithKey
	regexp.MustCompile(`^foldrWithKey$`),     // Data.Map.foldrWithKey
	regexp.MustCompile(`^filterWithKey$`),    // Data.Map.filterWithKey
	regexp.MustCompile(`^elems$`),            // Data.Map.elems :: Map k v -> [v]
	regexp.MustCompile(`^keys$`),             // Data.Map.keys :: Map k v -> [k]
	regexp.MustCompile(`^keysSet$`),          // Data.Map.keysSet :: Map k v -> Set k
	regexp.MustCompile(`^member$`),           // Data.Map.member :: Ord k => k -> Map k v -> Bool
	regexp.MustCompile(`^notMember$`),        // Data.Map.notMember :: Ord k => k -> Map k v -> Bool
	regexp.MustCompile(`^singleton$`),        // Data.Map.singleton :: k -> v -> Map k v
	regexp.MustCompile(`^unionsWith$`),       // Data.Map.unionsWith

	// ── Data.List ─────────────────────────────────────────────────────
	regexp.MustCompile(`^sortBy$`),       // Data.List.sortBy :: (a -> a -> Ordering) -> [a] -> [a]
	regexp.MustCompile(`^sortOn$`),       // Data.List.sortOn :: Ord b => (a -> b) -> [a] -> [a]
	regexp.MustCompile(`^groupBy$`),      // Data.List.groupBy
	regexp.MustCompile(`^nub$`),          // Data.List.nub :: Eq a => [a] -> [a]
	regexp.MustCompile(`^nubBy$`),        // Data.List.nubBy
	regexp.MustCompile(`^partition$`),    // Data.List.partition :: (a -> Bool) -> [a] -> ([a], [a])
	regexp.MustCompile(`^isPrefixOf$`),   // Data.List.isPrefixOf
	regexp.MustCompile(`^isSuffixOf$`),   // Data.List.isSuffixOf
	regexp.MustCompile(`^isInfixOf$`),    // Data.List.isInfixOf
	regexp.MustCompile(`^intercalate$`),  // Data.List.intercalate :: [a] -> [[a]] -> [a]
	regexp.MustCompile(`^intersperse$`),  // Data.List.intersperse :: a -> [a] -> [a]
	regexp.MustCompile(`^transpose$`),    // Data.List.transpose :: [[a]] -> [[a]]
	regexp.MustCompile(`^permutations$`), // Data.List.permutations :: [a] -> [[a]]
	regexp.MustCompile(`^subsequences$`), // Data.List.subsequences :: [a] -> [[a]]
	regexp.MustCompile(`^tails$`),        // Data.List.tails :: [a] -> [[a]]
	regexp.MustCompile(`^inits$`),        // Data.List.inits :: [a] -> [[a]]
	regexp.MustCompile(`^stripPrefix$`),  // Data.List.stripPrefix

	// ── Text.Printf ────────────────────────────────────────────────────
	regexp.MustCompile(`^printf$`),  // Text.Printf.printf :: PrintfType r => String -> r
	regexp.MustCompile(`^hPrintf$`), // Text.Printf.hPrintf
	regexp.MustCompile(`^sprintf$`), // Text.Printf.sprintf

	// ── Data.Maybe ──────────────────────────────────────────────────────
	regexp.MustCompile(`^fromMaybe$`),   // Data.Maybe.fromMaybe :: a -> Maybe a -> a
	regexp.MustCompile(`^isJust$`),      // Data.Maybe.isJust :: Maybe a -> Bool
	regexp.MustCompile(`^isNothing$`),   // Data.Maybe.isNothing :: Maybe a -> Bool
	regexp.MustCompile(`^fromJust$`),    // Data.Maybe.fromJust :: Maybe a -> a
	regexp.MustCompile(`^catMaybes$`),   // Data.Maybe.catMaybes :: [Maybe a] -> [a]
	regexp.MustCompile(`^mapMaybe$`),    // Data.Maybe.mapMaybe :: (a -> Maybe b) -> [a] -> [b]
	regexp.MustCompile(`^listToMaybe$`), // Data.Maybe.listToMaybe :: [a] -> Maybe a
	regexp.MustCompile(`^maybeToList$`), // Data.Maybe.maybeToList :: Maybe a -> [a]
	regexp.MustCompile(`^maybe$`),       // Prelude.maybe :: b -> (a -> b) -> Maybe a -> b

	// ── System.IO / IORef ────────────────────────────────────────────────
	regexp.MustCompile(`^newIORef$`),     // Data.IORef.newIORef :: a -> IO (IORef a)
	regexp.MustCompile(`^readIORef$`),    // Data.IORef.readIORef :: IORef a -> IO a
	regexp.MustCompile(`^writeIORef$`),   // Data.IORef.writeIORef :: IORef a -> a -> IO ()
	regexp.MustCompile(`^modifyIORef$`),  // Data.IORef.modifyIORef :: IORef a -> (a -> a) -> IO ()
	regexp.MustCompile(`^modifyIORef'$`), // Data.IORef.modifyIORef' (strict)
	regexp.MustCompile(`^hFlush$`),       // System.IO.hFlush :: Handle -> IO ()
	regexp.MustCompile(`^hPutStrLn$`),    // System.IO.hPutStrLn :: Handle -> String -> IO ()
	regexp.MustCompile(`^hGetLine$`),     // System.IO.hGetLine :: Handle -> IO String
	regexp.MustCompile(`^withFile$`),     // System.IO.withFile :: FilePath -> IOMode -> (Handle -> IO r) -> IO r
	regexp.MustCompile(`^openFile$`),     // System.IO.openFile :: FilePath -> IOMode -> IO Handle
	regexp.MustCompile(`^hClose$`),       // System.IO.hClose :: Handle -> IO ()

	// ── Control.Exception ────────────────────────────────────────────────
	regexp.MustCompile(`^catch$`),    // Control.Exception.catch :: Exception e => IO a -> (e -> IO a) -> IO a
	regexp.MustCompile(`^try$`),      // Control.Exception.try :: Exception e => IO a -> IO (Either e a)
	regexp.MustCompile(`^evaluate$`), // Control.Exception.evaluate :: a -> IO a
	regexp.MustCompile(`^throwIO$`),  // Control.Exception.throwIO :: Exception e => e -> IO a
	regexp.MustCompile(`^bracket$`),  // Control.Exception.bracket
	regexp.MustCompile(`^finally$`),  // Control.Exception.finally :: IO a -> IO b -> IO a
	regexp.MustCompile(`^handle$`),   // Control.Exception.handle (flipped catch)

	// ── Data.Char ──────────────────────────────────────────────────────
	regexp.MustCompile(`^isAlpha$`),    // Data.Char.isAlpha
	regexp.MustCompile(`^isAlphaNum$`), // Data.Char.isAlphaNum
	regexp.MustCompile(`^isDigit$`),    // Data.Char.isDigit
	regexp.MustCompile(`^isSpace$`),    // Data.Char.isSpace
	regexp.MustCompile(`^isUpper$`),    // Data.Char.isUpper
	regexp.MustCompile(`^isLower$`),    // Data.Char.isLower
	regexp.MustCompile(`^toUpper$`),    // Data.Char.toUpper
	regexp.MustCompile(`^toLower$`),    // Data.Char.toLower
	regexp.MustCompile(`^ord$`),        // Data.Char.ord :: Char -> Int
	regexp.MustCompile(`^chr$`),        // Data.Char.chr :: Int -> Char

	// ── STM / Concurrency ──────────────────────────────────────────────
	regexp.MustCompile(`^atomically$`),  // Control.Concurrent.STM.atomically
	regexp.MustCompile(`^newTVar$`),     // Control.Concurrent.STM.newTVar
	regexp.MustCompile(`^readTVar$`),    // Control.Concurrent.STM.readTVar
	regexp.MustCompile(`^writeTVar$`),   // Control.Concurrent.STM.writeTVar
	regexp.MustCompile(`^modifyTVar$`),  // Control.Concurrent.STM.modifyTVar
	regexp.MustCompile(`^newMVar$`),     // Control.Concurrent.MVar.newMVar
	regexp.MustCompile(`^readMVar$`),    // Control.Concurrent.MVar.readMVar
	regexp.MustCompile(`^takeMVar$`),    // Control.Concurrent.MVar.takeMVar
	regexp.MustCompile(`^putMVar$`),     // Control.Concurrent.MVar.putMVar
	regexp.MustCompile(`^withMVar$`),    // Control.Concurrent.MVar.withMVar
	regexp.MustCompile(`^forkIO$`),      // Control.Concurrent.forkIO :: IO () -> IO ThreadId
	regexp.MustCompile(`^threadDelay$`), // Control.Concurrent.threadDelay :: Int -> IO ()
}

func init() {
	dynamicPatternsByLang["haskell"] = haskellDynamicPatterns
}
