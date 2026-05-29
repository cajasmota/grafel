"""MongoEngine ODM relationship fixture — issue #3070."""
import mongoengine as me
from mongoengine import (
    Document,
    EmbeddedDocument,
    ReferenceField,
    EmbeddedDocumentField,
    EmbeddedDocumentListField,
    LazyReferenceField,
    StringField,
    IntField,
)


class Address(EmbeddedDocument):
    street = StringField()
    city = StringField()


class Category(Document):
    name = StringField()

    meta = {"collection": "categories"}


class Author(Document):
    name = StringField()

    meta = {"collection": "authors"}


class Book(Document):
    title = StringField()
    author = ReferenceField(Author)
    category = LazyReferenceField(Category)
    address = EmbeddedDocumentField(Address)

    meta = {"collection": "books"}


class Library(Document):
    name = StringField()
    books = EmbeddedDocumentListField(Book)

    meta = {"collection": "libraries"}
