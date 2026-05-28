// React Native CLI native-module autolinking config. Issue #2879 fixture.
module.exports = {
  project: {
    ios: {},
    android: {},
  },
  assets: ['./assets/fonts'],
  dependencies: {
    'react-native-vector-icons': {
      platforms: {
        ios: null,
      },
    },
    'react-native-ble-plx': {},
  },
};
