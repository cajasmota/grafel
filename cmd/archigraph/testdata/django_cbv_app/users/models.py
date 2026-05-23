from django.db import models

class User(models.Model):
    email = models.EmailField()
    name = models.CharField(max_length=100)
