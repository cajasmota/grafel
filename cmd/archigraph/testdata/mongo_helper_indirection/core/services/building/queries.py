from pymongo import ASCENDING, DESCENDING


def get_inspection_devices_pipeline(params: dict):
    building_id = params.get("building_id")
    user_groups = params.get("user_groups")

    pipeline = [
        {
            "$lookup": {
                "from": "inspection_groups",
                "localField": "inspectionGroupId",
                "foreignField": "_id",
                "as": "inspections_group",
            }
        },
        {"$unwind": {"path": "$inspections_group", "preserveNullAndEmptyArrays": True}},
        {"$match": {"building_id": building_id, "group_id": {"$in": user_groups}}},
        {
            "$lookup": {
                "from": "m_devices",
                "let": {"device_id": "$device_id"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$device_id"]}}},
                    {
                        "$lookup": {
                            "from": "m_group_device_settings",
                            "let": {"device_id": "$$device_id"},
                            "pipeline": [
                                {"$match": {"$expr": {"$eq": ["$device_id", "$$device_id"]}}},
                            ],
                            "as": "group_device_settings",
                        }
                    },
                ],
                "as": "device",
            }
        },
        {"$unwind": {"path": "$device", "preserveNullAndEmptyArrays": True}},
        {
            "$lookup": {
                "from": "m_contracts",
                "localField": "contractId",
                "foreignField": "_id",
                "as": "contract",
            }
        },
        {
            "$project": {
                "_id": 1,
                "contractId": 1,
            }
        },
    ]
    return pipeline
