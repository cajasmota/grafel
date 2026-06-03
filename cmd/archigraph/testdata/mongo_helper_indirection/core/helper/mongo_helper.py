from pymongo import MongoClient
from pymongo.collection import Collection


class MongoDBConnection:
    _client = None

    @classmethod
    def get_client(cls):
        if cls._client is None:
            cls._client = MongoClient("mongodb://localhost:27017")
        return cls._client

    @classmethod
    def get_collection(cls, collection_name, db_name="app"):
        client = cls.get_client()
        db = client[db_name]
        collection = db[collection_name]
        return collection
