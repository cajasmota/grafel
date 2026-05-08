import React from 'react';
import { View, Text } from 'react-native';
import Header from './components/Header';
import Content from './components/Content';
import Footer from './components/Footer';

export default function HomeScreen() {
  return (
    <View>
      <Header title="Home" />
      <Content>
        <Text>Welcome to the Home Screen</Text>
      </Content>
      <Footer />
    </View>
  );
}
