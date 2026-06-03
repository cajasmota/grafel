from core.mongodb_collections import INSPECTIONS, INSPECTIONS_GROUP
from core.helper.mongo_helper import MongoDBConnection
from core.services.building.queries import (
    get_inspection_devices_pipeline,
)


def get_inspection_devices(params: dict):
    inspections_cln = MongoDBConnection.get_collection(INSPECTIONS)
    data = inspections_cln.aggregate(get_inspection_devices_pipeline(params)).next()

    if not data:
        return None

    return data
