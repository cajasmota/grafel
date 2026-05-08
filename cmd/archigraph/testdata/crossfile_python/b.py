"""Imports Greeter from a.py and exercises a cross-file CALLS edge."""

from a import Greeter


def main():
    g = Greeter()
    return g.greet("world")
