"""Defines the Greeter class and a helper function used by b.py."""


def make_message():
    return "hello"


class Greeter:
    def greet(self, name):
        return make_message() + " " + name

    def shout(self, name):
        return self.greet(name).upper()
