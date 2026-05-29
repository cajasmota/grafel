"""Tortoise ORM relationship fixture — issue #3070."""
from tortoise import fields
from tortoise.models import Model


class Tournament(Model):
    name = fields.CharField(max_length=255)
    events: fields.ReverseRelation["Event"]

    class Meta:
        table = "tournament"


class Event(Model):
    name = fields.CharField(max_length=255)
    tournament: fields.ForeignKeyRelation[Tournament] = fields.ForeignKeyField(
        "models.Tournament", related_name="events"
    )
    participants: fields.ManyToManyRelation["Team"] = fields.ManyToManyField(
        "models.Team", related_name="events", through="event_team"
    )

    class Meta:
        table = "event"


class Team(Model):
    name = fields.CharField(max_length=255)
    events: fields.ManyToManyRelation[Event]

    class Meta:
        table = "team"


class Player(Model):
    name = fields.CharField(max_length=255)
    team = fields.ForeignKeyField("models.Team", related_name="players")
    profile = fields.OneToOneField("models.Profile", related_name="player", null=True)

    class Meta:
        table = "player"


class Profile(Model):
    bio = fields.TextField()

    class Meta:
        table = "profile"
