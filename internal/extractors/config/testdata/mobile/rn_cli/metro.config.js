// Metro bundler config (React Native CLI). Issue #2879 fixture.
const { getDefaultConfig, mergeConfig } = require('@react-native/metro-config');

const config = {
  projectRoot: __dirname,
  watchFolders: ['../shared'],
  transformer: {
    babelTransformerPath: require.resolve('react-native-svg-transformer'),
  },
  resolver: {
    sourceExts: ['js', 'jsx', 'ts', 'tsx', 'svg'],
    alias: {
      '@app': './src',
    },
  },
};

module.exports = mergeConfig(getDefaultConfig(__dirname), config);
