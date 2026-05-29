"""Peewee ORM relationship fixture — issue #3070."""
import peewee
from peewee import Model, ForeignKeyField, ManyToManyField, CharField, IntegerField

database = peewee.SqliteDatabase("test.db")


class Author(Model):
    name = CharField()

    class Meta:
        database = database


class Book(Model):
    title = CharField()
    author = ForeignKeyField(Author, backref="books")
    year = IntegerField()

    class Meta:
        database = database


class Tag(Model):
    name = CharField(unique=True)

    class Meta:
        database = database


class BookTag(Model):
    book = ForeignKeyField(Book)
    tag = ForeignKeyField(Tag)

    class Meta:
        database = database


class Article(Model):
    title = CharField()
    tags = ManyToManyField(Tag, backref="articles")

    class Meta:
        database = database
