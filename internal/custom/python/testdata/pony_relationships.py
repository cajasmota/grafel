"""Pony ORM relationship fixture — issue #3070."""
from pony.orm import Database, Required, Optional, Set, PrimaryKey

db = Database()


class Department(db.Entity):
    name = Required(str)
    employees = Set("Employee")


class Employee(db.Entity):
    name = Required(str)
    department = Required(Department)
    manager = Optional("Employee", reverse="subordinates")
    subordinates = Set("Employee", reverse="manager")
    projects = Set("Project")


class Project(db.Entity):
    title = Required(str)
    members = Set(Employee)
