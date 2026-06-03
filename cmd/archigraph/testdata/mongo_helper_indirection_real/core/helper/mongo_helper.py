from pymongo import MongoClient, ASCENDING, InsertOne
from django.conf import settings
from pymongo.errors import PyMongoError
from pymongo.collection import Collection
import os


class MongoDBConnection:
    _client = None
    _pid = None

    @classmethod
    def get_client(cls):
        pid = os.getpid()
        if cls._pid != pid:
            cls._client = None

        if cls._client is None:
            mongo_user = settings.MONGO_DB_USER
            mongo_password = settings.MONGO_DB_PASSWORD
            mongo_host = settings.MONGO_DB_HOST
            mongo_db_name = settings.MONGO_DB_NAME
            cls._client = MongoClient(
                f"mongodb+srv://{mongo_user}:{mongo_password}@{mongo_host}/{mongo_db_name}?retryWrites=true&w=majority&tlsInsecure=true"
            )
            cls._pid = pid
        return cls._client

    @classmethod
    def get_collection(cls, collection_name, db_name=settings.MONGO_DB_NAME):
        client = cls.get_client()
        db = client[db_name]
        collection = db[collection_name]

        if isinstance(collection, Collection):
            # Save original methods
            original_insert_one = collection.insert_one
            original_insert_many = collection.insert_many
            original_bulk_write = collection.bulk_write

            # insert_one override
            def custom_insert_one(data, *args, **kwargs):
                if "id" not in data:
                    data["id"] = cls.get_next_sequence(collection_name)
                return original_insert_one(data, *args, **kwargs)

            # insert_many override
            def custom_insert_many(data_list, *args, **kwargs):
                for item in data_list:
                    if "id" not in item:
                        item["id"] = cls.get_next_sequence(collection_name)
                return original_insert_many(data_list, *args, **kwargs)

            # bulk_write override
            def custom_bulk_write(operations, *args, **kwargs):
                updated_ops = []
                for op in operations:
                    if isinstance(op, InsertOne):
                        doc = op._doc
                        if "id" not in doc:
                            doc["id"] = cls.get_next_sequence(collection_name)
                        updated_ops.append(InsertOne(doc))
                    else:
                        updated_ops.append(op)
                return original_bulk_write(updated_ops, *args, **kwargs)

            # Apply patches safely
            collection.insert_one = custom_insert_one
            collection.insert_many = custom_insert_many
            collection.bulk_write = custom_bulk_write
        return collection

    @classmethod
    def get_all_database(cls):
        client = cls.get_client()
        return client.list_database_names()

    @classmethod
    def get_all_collections(cls, db_name):
        client = cls.get_client()
        db = client[db_name]
        return db.list_collection_names()

    @classmethod
    def insert_one(cls, collection_name, data):
        if "id" not in data:
            data["id"] = cls.get_next_sequence(collection_name)
        collection = cls.get_collection(collection_name)
        return collection.insert_one(data)

    @classmethod
    def insert_many(cls, collection_name, data):
        for item in data:
            if "id" not in item:
                item["id"] = cls.get_next_sequence(collection_name)
        collection = cls.get_collection(collection_name)
        return collection.insert_many(data)

    @classmethod
    def find_one(cls, collection_name, query):
        collection = cls.get_collection(collection_name)
        return collection.find_one(query)

    @classmethod
    def find_many(cls, collection_name, query):
        collection = cls.get_collection(collection_name)
        return collection.find(query)

    @classmethod
    def update_one(cls, collection_name, query, new_values, **args):
        collection = cls.get_collection(collection_name)
        return collection.update_one(query, new_values, **args)

    @classmethod
    def update_many(cls, collection_name, query, new_values):
        collection = cls.get_collection(collection_name)
        return collection.update_many(query, new_values)

    @classmethod
    def delete_one(cls, collection_name, query):
        collection = cls.get_collection(collection_name)
        return collection.delete_one(query)
    
    @classmethod
    def count_documents(cls, collection_name, query):
        collection = cls.get_collection(collection_name)
        return collection.count_documents(query)

    @classmethod
    def delete_many(cls, collection_name, query):
        collection = cls.get_collection(collection_name)
        return collection.delete_many(query)
    
    @classmethod
    def find_one_and_delete(cls, collection_name, query):
        collection = cls.get_collection(collection_name)
        return collection.find_one_and_delete(query)
    
    @classmethod
    def find_one_and_update(cls, collection_name, query, new_values):
        collection = cls.get_collection(collection_name)
        return collection.find_one_and_update(query, new_values)

    @classmethod
    def drop_collection(cls, collection_name):
        collection = cls.get_collection(collection_name)
        return collection.drop()

    @classmethod
    def drop_database(cls, db_name):
        client = cls.get_client()
        return client.drop_database(db_name)

    @classmethod
    def close_connection(cls):
        if cls._client is not None:
            cls._client.close()
            cls._client = None

    @classmethod
    def empty_collection(cls, collection_name):
        collection = cls.get_collection(collection_name)
        collection.delete_many({})
        print(f"All documents in the '{collection_name}' collection have been deleted.")

    @classmethod
    def create_index(cls, collection_name, field_name):
        """
        Creates an index on the specified field in the given collection.

        :param collection_name: The name of the collection to create the index on.
        :param field_name: The field to index.
        """
        try:
            collection = cls.get_collection(collection_name)
            index_name = collection.create_index([(field_name, ASCENDING)])
            print(
                f"Index '{index_name}' created on field '{field_name}' in collection '{collection_name}'."
            )
        except PyMongoError as e:
            print(
                f"Error creating index on field '{field_name}' in collection '{collection_name}': {str(e)}"
            )
    
    @classmethod
    def get_next_sequence(cls, sequence_name):
        counters_collection = cls.get_collection("counters")
        result = counters_collection.find_one_and_update(
            {"_id": sequence_name},
            {"$inc": {"sequence_value": 1}},
            upsert=True,
            return_document=True
        )
        return result["sequence_value"]
