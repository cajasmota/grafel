from pymongo import ASCENDING, DESCENDING


def get_inspection_devices_pipeline(params: dict):

    building_id = params.get("building_id")
    user_groups = params.get("user_groups")
    devices = params.get("devices")
    clients = params.get("clients")
    statuses = params.get("statuses")
    elv3_compliance_status = params.get("elv3_compliance_status")
    aoc_compliance_status = params.get("aoc_compliance_report_status")
    inspection_types = params.get("inspection_types")
    offset = params.get("offset")
    limit = params.get("limit")
    sort = params.get("field")
    order = params.get("order")
    order = DESCENDING if order == "descend" else ASCENDING

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
        {"$set": {"building_id": "$inspections_group.buildingId", "group_id": "$inspections_group.groupId"}},
        {"$match": {"building_id": building_id, "group_id": {"$in": user_groups}}},
        {
            "$project": {
                "_id": 1,
                "contractId": 1,
                "contractNumber": 1,
                "contractType": 1,
                "inspectionGroupId": 1,
                "device_id": 1,
                "device_name": 1,
                "fsotDate": 1,
                "result": 1,
                "status": 1,
                "reason": 1,
                "inspection_date": 1,
                "is_reinspected": 1,
                "inspections_group": 1,
                "inspection_checklists": {"$first": "$inspection_checklists"},
                "elv3": 1,
                "aoc": 1,
                "aoc_filed_date": 1,
            }
        },
        {
            "$set": {
                "users_to_lookup": [
                    "$inspections_group.inspectorId",
                    "$inspections_group.performingInspectorId",
                    "$inspections_group.witnessingInspectorId",
                    "$inspections_group.performingDirectorId",
                    "$inspections_group.witnessingDirectorId",
                ]
            }
        },
        {
            "$lookup": {
                "from": "m_devices",
                "let": {"device_id": "$device_id", "group_id": "$group_id"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$device_id"]}}},
                    {"$limit": 1},
                    {
                        "$lookup": {
                            "from": "m_group_device_settings",
                            "let": {"device_id": "$$device_id", "group_id": "$$group_id"},
                            "pipeline": [
                                {
                                    "$match": {
                                        "$expr": {
                                            "$and": [
                                                {"$eq": ["$device_id", "$$device_id"]},
                                                {"$eq": ["$group_id", "$$group_id"]},
                                            ]
                                        }
                                    }
                                },
                                {"$limit": 1},
                                {
                                    "$project": {
                                        "_id": 0,
                                        "name": 1,
                                    }
                                },
                            ],
                            "as": "group_device_settings",
                        }
                    },
                    {"$unwind": {"path": "$group_device_settings", "preserveNullAndEmptyArrays": True}},
                    {
                        "$project": {
                            "_id": 0,
                            "id": "$postgresql_id",
                            "name": {"$ifNull": ["$group_device_settings.name", "$name"]},
                        }
                    },
                ],
                "as": "device",
            }
        },
        {"$unwind": {"path": "$device", "preserveNullAndEmptyArrays": True}},
        {
            "$lookup": {
                "from": "m_users",
                "let": {"ids": "$users_to_lookup"},
                "pipeline": [
                    {"$match": {"$expr": {"$in": ["$postgresql_id", "$$ids"]}}},
                    {"$project": {"_id": 0, "id": "$postgresql_id", "first_name": 1, "last_name": 1}},
                ],
                "as": "users",
            }
        },
        {
            "$set": {
                "inspector": {
                    "$first": {
                        "$filter": {
                            "input": "$users",
                            "as": "u",
                            "cond": {"$eq": ["$$u.id", "$inspections_group.inspectorId"]},
                        }
                    }
                },
                "performing_inspector": {
                    "$first": {
                        "$filter": {
                            "input": "$users",
                            "as": "u",
                            "cond": {"$eq": ["$$u.id", "$inspections_group.performingInspectorId"]},
                        }
                    }
                },
                "performing_director": {
                    "$first": {
                        "$filter": {
                            "input": "$users",
                            "as": "u",
                            "cond": {"$eq": ["$$u.id", "$inspections_group.performingDirectorId"]},
                        }
                    }
                },
                "witnessing_inspector": {
                    "$first": {
                        "$filter": {
                            "input": "$users",
                            "as": "u",
                            "cond": {"$eq": ["$$u.id", "$inspections_group.witnessingInspectorId"]},
                        }
                    }
                },
                "witnessing_director": {
                    "$first": {
                        "$filter": {
                            "input": "$users",
                            "as": "u",
                            "cond": {"$eq": ["$$u.id", "$inspections_group.witnessingDirectorId"]},
                        }
                    }
                },
            }
        },
        {
            "$lookup": {
                "from": "m_contracts",
                "let": {"contract_id": "$contractId"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$contract_id"]}}},
                    {
                        "$project": {
                            "_id": 0,
                            "id": "$postgresql_id",
                            "contract_number": 1,
                            "contract_type": 1,
                            "selected_address_type": 1,
                            "selected_address": 1,
                            "client_id": 1,
                        }
                    },
                    {"$limit": 1},
                ],
                "as": "contract",
            }
        },
        {"$unwind": {"path": "$contract", "preserveNullAndEmptyArrays": True}},
        {
            "$lookup": {
                "from": "m_building_alternate_adresses",
                "localField": "selected_address",
                "foreignField": "postgresql_id",
                "as": "alternate_adresses",
            }
        },
        {"$unwind": {"path": "$alternate_adresses", "preserveNullAndEmptyArrays": True}},
        {
            "$lookup": {
                "from": "m_buildings",
                "let": {"building_id": "$inspections_group.buildingId"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$building_id"]}}},
                    {
                        "$project": {
                            "_id": 0,
                            "id": "$postgresql_id",
                            "name": 1,
                            "physical_address": 1,
                            "premises_address": 1,
                        }
                    },
                    {"$limit": 1},
                ],
                "as": "building",
            }
        },
        {"$unwind": {"path": "$building", "preserveNullAndEmptyArrays": True}},
        {
            "$lookup": {
                "from": "m_clients",
                "let": {"client_id": {"$ifNull": ["$inspections_group.clientId", "$contract.client_id"]}},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$client_id"]}}},
                    {"$project": {"_id": 0, "id": "$postgresql_id", "name": 1}},
                    {"$limit": 1},
                ],
                "as": "client",
            }
        },
        {"$unwind": {"path": "$client", "preserveNullAndEmptyArrays": True}},
        {
            "$lookup": {
                "from": "m_jurisdictions",
                "let": {"jurisdiction_id": "$inspections_group.jurisdictionId"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$jurisdiction_id"]}}},
                    {"$project": {"_id": 0, "id": "$postgresql_id", "name": 1}},
                    {"$limit": 1},
                ],
                "as": "jurisdiction",
            }
        },
        {"$unwind": {"path": "$jurisdiction", "preserveNullAndEmptyArrays": True}},
        {
            "$set": {
                "client_id": "$client.id",
                "checklist_id": "$inspection_checklists.id",
                "checklist_type": "$inspection_checklists.type",
                "elv3_compliance_status": {"$first": "$elv3.status"},
                "aoc_compliance_status": {"$first": "$aoc.compliance_report_status"},
                "computed_building_address": {
                    "$cond": {
                        "if": {"$eq": ["$contract.selected_address_type", "Premises Address"]},
                        "then": {"$ifNull": ["$building.premises_address", "$building.physical_address"]},
                        "else": {"$ifNull": ["$alternate_adresses.address", "$building.physical_address"]},
                    }
                },
            }
        },
    ]

    if statuses:
        pipeline.append({"$match": {"status": {"$in": statuses}}})

    if inspection_types:
        pipeline.append({"$match": {"checklist_type": {"$in": list(map(int, inspection_types))}}})

    if clients:
        pipeline.append({"$match": {"client_id": {"$in": list(map(int, clients))}}})

    if devices:
        pipeline.append({"$match": {"device_id": {"$in": list(map(int, devices))}}})

    if elv3_compliance_status:
        pipeline.append({"$match": {"elv3_compliance_status": {"$in": elv3_compliance_status}}})

    if aoc_compliance_status:
        pipeline.append({"$match": {"aoc_compliance_status": {"$in": aoc_compliance_status}}})

    pipeline.extend(
        [
            {
                "$lookup": {
                    "from": "inspections_history",
                    "let": {
                        "inspection_id": "$_id",
                        "client_name": "$client.name",
                        "checklist_id": "$checklist_id",
                        "checklist_type": "$checklist_type",
                        "inspection_date": "$inspection_date",
                        "inspector_id": "$inspector_id",
                        "contract_id": "$contract.id",
                        "contract_type": "$contract.contract_type",
                        "contract_number": "$contract.contract_number",
                        "building_address": "$computed_building_address",
                        "performing_inspector_id": "$performing_inspector.id",
                        "performing_director_id": "$performing_director.id",
                        "witnessing_inspector_id": "$witnessing_inspector.id",
                        "witnessing_director_id": "$witnessing_director.id",
                        "elevator_company_id": "$elevator_company.id",
                        "witnessing_company_id": "$witnessing_company.id",
                        "scheduled_date": "$inspections_group.startDate",
                    },
                    "pipeline": [
                        {"$match": {"$expr": {"$eq": ["$parent_id", "$$inspection_id"]}}},
                        {
                            "$project": {
                                "_id": 0,
                                "id": {"$toString": "$_id"},
                                "contractId": 1,
                                "contractNumber": 1,
                                "contractType": 1,
                                "device_id": 1,
                                "device_name": 1,
                                "fsotDate": 1,
                                "result": 1,
                                "status": 1,
                                "reason": 1,
                                "inspection_date": "$$inspection_date",
                                "inspection_files": 1,
                                "inspection_emails": 1,
                                "inspection_notes": 1,
                                "inspection_checklists": 1,
                                "client_name": "$$client_name",
                                "checklist_id": "$$checklist_id",
                                "checklist_type": "$$checklist_type",
                                "contract_id": "$$contract_id",
                                "contract_type": "$$contract_type",
                                "contract_number": "$$contract_number",
                                "building_address": "$$building_address",
                                "inspector_id": "$$inspector_id",
                                "scheduled_date": "$$scheduled_date",
                                "performing_inspector_id": "$$performing_inspector_id",
                                "performing_director_id": "$$performing_director_id",
                                "witnessing_inspector_id": "$$witnessing_inspector_id",
                                "witnessing_director_id": "$$witnessing_director_id",
                                "elevator_company_id": "$$elevator_company_id",
                                "witnessing_company_id": "$$witnessing_company_id",
                                "elv3": 1,
                                "aoc": 1,
                            }
                        },
                        {"$limit": 1},
                    ],
                    "as": "history",
                }
            },
            {
                "$project": {
                    "_id": 0,
                    "id": {"$toString": "$_id"},
                    "inspections_group_id": {"$toString": "$inspections_group._id"},
                    "device_id": "$device.id",
                    "device_name": "$device.name",
                    "status": "$status",
                    "scheduled_date": "$inspections_group.startDate",
                    "fsot_date": {"$ifNull": ["$inspections_group.fsotDate", None]},
                    "result": "$result",
                    "inspection_date": "$inspection_date",
                    "checklist_id": "$checklist_id",
                    "checklist_type": "$checklist_type",
                    "is_reinspected": {"$ifNull": ["$is_reinspected", False]},
                    "inspector_id": {"$ifNull": ["$inspector.id", "$inspections_group.inspectorId"]},
                    "inspector_name": {
                        "$trim": {"input": {"$concat": ["$inspector.first_name", " ", "$inspector.last_name"]}}
                    },
                    "performing_inspector_id": {"$ifNull": ["$performing_inspector.id", None]},
                    "performing_inspector_name": {
                        "$trim": {
                            "input": {
                                "$concat": ["$performing_inspector.first_name", " ", "$performing_inspector.last_name"]
                            }
                        }
                    },
                    "performing_director_id": {
                        "$ifNull": ["$performing_director.id", "$inspections_group.performingDirectorId"]
                    },
                    "performing_director_name": {
                        "$cond": {
                            "if": "$performing_director.id",
                            "then": {
                                "$trim": {
                                    "input": {
                                        "$concat": [
                                            "$performing_director.first_name",
                                            " ",
                                            "$performing_director.last_name",
                                        ]
                                    }
                                }
                            },
                            "else": None,
                        }
                    },
                    "witnessing_inspector_id": {
                        "$ifNull": ["$witnessing_inspector.id", "$inspections_group.witnessingInspectorId"]
                    },
                    "witnessing_inspector_name": {
                        "$trim": {
                            "input": {
                                "$concat": ["$witnessing_inspector.first_name", " ", "$witnessing_inspector.last_name"]
                            }
                        }
                    },
                    "witnessing_director_id": {
                        "$ifNull": ["$witnessing_director.id", "$inspections_group.witnessingDirectorId"]
                    },
                    "witnessing_director_name": {
                        "$trim": {
                            "input": {
                                "$concat": ["$witnessing_director.first_name", " ", "$witnessing_director.last_name"]
                            }
                        }
                    },
                    "witnessing_company_id": {"$ifNull": ["$inspections_group.witnessingCompanyId", None]},
                    "elevator_company_id": {"$ifNull": ["$inspections_group.elevator_company_id", None]},
                    "jurisdiction_id": "$jurisdiction.id",
                    "jurisdiction_name": "$jurisdiction.name",
                    "building_id": "$building.id",
                    "building_address": "$computed_building_address",
                    "client_id": "$client.id",
                    "client_name": {"$ifNull": ["$client.name", "$inspections_group.clientName"]},
                    "contract_id": "$contract.id",
                    "contract_number": "$contract.contract_number",
                    "contract_type": "$contract.contract_type",
                    "aoc_filed_date": "$aoc_filed_date",
                    "children": {
                        "$cond": [{"$gt": [{"$size": {"$ifNull": ["$history", []]}}, 0]}, "$history", "$$REMOVE"]
                    },
                    "aoc": {"$ifNull": ["$aoc", []]},
                    "elv3": {"$ifNull": ["$elv3", []]},
                }
            },
            {
                "$facet": {
                    "results": [
                        {"$sort": {sort: order}},
                        {"$skip": offset},
                        {"$limit": limit},
                    ],
                    "count": [{"$count": "total"}],
                }
            },
            {"$project": {"results": 1, "count": {"$arrayElemAt": ["$count.total", 0]}}},
        ]
    )

    return pipeline


def get_inspection_devices_filters_pipeline(params: dict):

    building_id = params.get("building_id")
    user_groups = params.get("user_groups")

    return [
        {
            "$lookup": {
                "from": "inspection_groups",
                "localField": "inspectionGroupId",
                "foreignField": "_id",
                "as": "inspections_group",
            }
        },
        {"$unwind": {"path": "$inspections_group", "preserveNullAndEmptyArrays": True}},
        {"$set": {"building_id": "$inspections_group.buildingId", "group_id": "$inspections_group.groupId"}},
        {"$match": {**({"building_id": building_id} if building_id else {}), "group_id": {"$in": user_groups}}},
        {
            "$project": {
                "_id": 1,
                "device_id": 1,
                "device_name": 1,
                "status": 1,
                "inspections_group": 1,
                "inspection_checklists": {"$first": "$inspection_checklists"},
                "aoc": {"$first": "$aoc"},
                "elv3": {"$first": "$elv3"},
            }
        },
        {
            "$lookup": {
                "from": "m_clients",
                "let": {"client_id": "$inspections_group.clientId"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$client_id"]}}},
                    {"$project": {"_id": 0, "id": "$postgresql_id", "name": 1}},
                    {"$limit": 1},
                ],
                "as": "client",
            }
        },
        {"$unwind": {"path": "$client", "preserveNullAndEmptyArrays": True}},
        {"$set": {"users_to_lookup": ["$inspections_group.inspectorId", "$inspections_group.witnessingInspectorId"]}},
        {
            "$lookup": {
                "from": "m_users",
                "let": {"ids": "$users_to_lookup"},
                "pipeline": [
                    {"$match": {"$expr": {"$in": ["$postgresql_id", "$$ids"]}}},
                    {"$project": {"_id": 0, "id": "$postgresql_id", "first_name": 1, "last_name": 1}},
                ],
                "as": "users",
            }
        },
        {
            "$set": {
                "inspector": {
                    "$first": {
                        "$filter": {
                            "input": "$users",
                            "as": "u",
                            "cond": {"$eq": ["$$u.id", "$inspections_group.inspectorId"]},
                        }
                    }
                },
                "witnessing_inspector": {
                    "$first": {
                        "$filter": {
                            "input": "$users",
                            "as": "u",
                            "cond": {"$eq": ["$$u.id", "$inspections_group.witnessingInspectorId"]},
                        }
                    }
                },
            }
        },
        {
            "$project": {
                "_id": 0,
                "device_id": "$device_id",
                "device_name": "$device_name",
                "status": "$status",
                "result": "$result",
                "client_id": "$client.id",
                "client_name": "$client.name",
                "inspector_id": {"$ifNull": ["$inspector.id", None]},
                "inspector_name": {
                    "$trim": {
                        "input": {
                            "$concat": [
                                {"$ifNull": ["$inspector.first_name", ""]},
                                " ",
                                {"$ifNull": ["$inspector.last_name", ""]},
                            ]
                        }
                    }
                },
                "witnessing_inspector_id": {"$ifNull": ["$witnessing_inspector.id", None]},
                "witnessing_inspector_name": {
                    "$trim": {
                        "input": {
                            "$concat": [
                                {"$ifNull": ["$witnessing_inspector.first_name", ""]},
                                " ",
                                {"$ifNull": ["$witnessing_inspector.last_name", ""]},
                            ]
                        }
                    }
                },
                "elv3_compliance_status": {"$ifNull": ["$elv3.status", None]},
                "aoc_compliance_status": {"$ifNull": ["$aoc.compliance_report_status", None]},
            }
        },
        {
            "$facet": {
                "devices": [
                    {"$match": {"device_id": {"$ne": None}, "device_name": {"$ne": None}, "device_name": {"$ne": ""}}},
                    {"$group": {"_id": "$device_id", "text": {"$first": "$device_name"}}},
                    {"$project": {"_id": 0, "value": "$_id", "text": 1}},
                    {"$sort": {"text": 1}},
                ],
                "clients": [
                    {"$match": {"client_id": {"$ne": None}, "client_name": {"$ne": None}, "client_name": {"$ne": ""}}},
                    {"$group": {"_id": "$client_id", "text": {"$first": "$client_name"}}},
                    {"$project": {"_id": 0, "value": "$_id", "text": 1}},
                    {"$sort": {"text": 1}},
                ],
                "statuses": [
                    {"$match": {"status": {"$ne": None}, "status": {"$ne": ""}}},
                    {"$group": {"_id": "$status"}},
                    {"$project": {"_id": 0, "value": "$_id", "text": "$_id"}},
                    {"$sort": {"text": 1}},
                ],
                "inspectors": [
                    {
                        "$match": {
                            "$or": [
                                {"inspector_id": {"$ne": None}},
                                {"witnessing_inspector_id": {"$ne": None}},
                            ]
                        }
                    },
                    {
                        "$project": {
                            "inspector_options": [
                                {"id": "$inspector_id", "name": "$inspector_name"},
                                {"id": "$witnessing_inspector_id", "name": "$witnessing_inspector_name"},
                            ]
                        }
                    },
                    {"$unwind": "$inspector_options"},
                    {"$match": {"inspector_options.id": {"$nin": [None, "", 0]}}},
                    {"$group": {"_id": "$inspector_options.id", "text": {"$first": "$inspector_options.name"}}},
                    {"$project": {"_id": 0, "value": "$_id", "text": 1}},
                    {"$sort": {"text": 1}},
                ],
                "elv3_compliance_status": [
                    {"$match": {"elv3_compliance_status": {"$nin": [None, ""]}}},
                    {"$group": {"_id": "$elv3_compliance_status"}},
                    {"$project": {"_id": 0, "value": "$_id", "text": "$_id"}},
                    {"$sort": {"text": 1}},
                ],
                "aoc_compliance_status": [
                    {"$match": {"aoc_compliance_status": {"$nin": [None, ""]}}},
                    {"$group": {"_id": "$aoc_compliance_status"}},
                    {"$project": {"_id": 0, "value": "$_id", "text": "$_id"}},
                    {"$sort": {"text": 1}},
                ],
            }
        },
    ]


def get_maintenance_evaluations_pipeline(params: dict):
    building_id = params.get("building_id")
    user_groups = params.get("user_groups")
    statuses = params.get("statuses")
    devices = params.get("devices")
    inspectors = params.get("inspectors")
    inspection_year = params.get("inspection_year")
    offset = params.get("offset")
    limit = params.get("limit")
    sort = params.get("field")
    order = params.get("order")
    order = DESCENDING if order == "descend" else ASCENDING
    search = params.get("search")
    checklist_id = params.get("checklist_id")

    if not building_id:
        statuses = ["Results Reviewed", "Assembling Report"]

    pipeline = [
        {
            "$project": {
                "_id": 1,
                "inspectionGroupId": 1,
                "contractId": 1,
                "device_id": 1,
                "result": 1,
                "status": 1,
                "checklist_id": {"$first": "$inspection_checklists.id"},
                "checklist_type": {"$first": "$inspection_checklists.type"},
                "inspection_year": {"$year": "$inspection_date"},
            }
        },
        {"$match": {"checklist_type": 4}},
        {
            "$lookup": {
                "from": "inspection_groups",
                "let": {"inspections_group_id": "$inspectionGroupId"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$_id", "$$inspections_group_id"]}}},
                    {
                        "$project": {
                            "_id": 1,
                            "groupId": 1,
                            "buildingId": 1,
                            "inspectorId": 1,
                            "clientId": 1,
                            "witnessingInspectorId": 1,
                            "startDate": 1,
                        }
                    },
                    {"$limit": 1},
                ],
                "as": "inspections_group",
            }
        },
        {
            "$unwind": {
                "path": "$inspections_group",
                "preserveNullAndEmptyArrays": True,
            }
        },
    ]

    if building_id:
        pipeline.append(
            {
                "$match": {
                    "inspections_group.buildingId": building_id,
                }
            }
        )

    pipeline.extend([
        {
            "$match": {
                "inspections_group.groupId": {"$in": user_groups},
            }
        },
        {
            "$set": {
                "users_to_lookup": [
                    "$inspections_group.inspectorId",
                    "$inspections_group.witnessingInspectorId",
                ]
            }
        },
        {
            "$lookup": {
                "from": "m_users",
                "let": {"ids": "$users_to_lookup"},
                "pipeline": [
                    {"$match": {"$expr": {"$in": ["$postgresql_id", "$$ids"]}}},
                    {
                        "$project": {
                            "_id": 0,
                            "id": "$postgresql_id",
                            "first_name": 1,
                            "last_name": 1,
                        }
                    },
                ],
                "as": "users",
            }
        },
        {
            "$set": {
                "inspector": {
                    "$ifNull": [
                        {
                            "$first": {
                                "$filter": {
                                    "input": "$users",
                                    "as": "u",
                                    "cond": {
                                        "$eq": [
                                            "$$u.id",
                                            "$inspections_group.inspectorId",
                                        ]
                                    },
                                }
                            }
                        },
                        None,
                    ]
                },
                "witnessing_inspector": {
                    "$ifNull": [
                        {
                            "$first": {
                                "$filter": {
                                    "input": "$users",
                                    "as": "u",
                                    "cond": {
                                        "$eq": [
                                            "$$u.id",
                                            "$inspections_group.witnessingInspectorId",
                                        ]
                                    },
                                }
                            }
                        },
                        None,
                    ]
                },
            }
        },
        {
            "$lookup": {
                "from": "m_devices",
                "let": {
                    "device_id": "$device_id",
                    "group_id": "$inspections_group.groupId",
                },
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$device_id"]}}},
                    {
                        "$lookup": {
                            "from": "m_group_device_settings",
                            "let": {
                                "device_id": "$$device_id",
                                "group_id": "$$group_id",
                            },
                            "pipeline": [
                                {
                                    "$match": {
                                        "$expr": {
                                            "$and": [
                                                {"$eq": ["$device_id", "$$device_id"]},
                                                {"$eq": ["$group_id", "$$group_id"]},
                                            ]
                                        }
                                    }
                                },
                                {"$limit": 1},
                                {
                                    "$project": {
                                        "_id": 0,
                                        "machine_number": 1,
                                        "car_number": 1,
                                        "equipment_type_id": 1,
                                    }
                                },
                            ],
                            "as": "settings",
                        }
                    },
                    {
                        "$unwind": {
                            "path": "$settings",
                            "preserveNullAndEmptyArrays": True,
                        }
                    },
                    {
                        "$lookup": {
                            "from": "m_device_equipment_types",
                            "let": {"equipment_type_id": "$settings.equipment_type_id"},
                            "pipeline": [
                                {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$equipment_type_id"]}}},
                                {"$project": {"_id": 0, "category": 1, "alias": 1}},
                                {"$limit": 1},
                            ],
                            "as": "equipment_type",
                        }
                    },
                    {"$unwind": {"path": "$equipment_type", "preserveNullAndEmptyArrays": True}},
                    {
                        "$project": {
                            "_id": 0,
                            "id": "$postgresql_id",
                            "name": {"$ifNull": ["$name", "$settings.name"]},
                            "machine_number": "$settings.machine_number",
                            "car_number": "$settings.car_number",
                            "equipment_type": {
                                "$cond": {
                                    "if": {"$ne": ["$equipment_type", None]},
                                    "then": {"$concat": ["$equipment_type.category", "-", "$equipment_type.alias"]},
                                    "else": None,
                                }
                            },
                        }
                    },
                ],
                "as": "device",
            }
        },
        {"$unwind": {"path": "$device", "preserveNullAndEmptyArrays": True}},
        {
            "$lookup": {
                "from": "m_buildings",
                "let": {"building_id": "$inspections_group.buildingId"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$building_id"]}}},
                    {"$limit": 1},
                    {
                        "$project": {
                            "_id": 0,
                            "id": "$postgresql_id",
                            "name": 1,
                            "premises_address": 1,
                        }
                    },
                ],
                "as": "building",
            }
        },
        {"$unwind": {"path": "$building", "preserveNullAndEmptyArrays": True}},
        {
            "$lookup": {
                "from": "m_contracts",
                "let": {"contract_id": "$contractId"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$contract_id"]}}},
                    {"$limit": 1},
                    {
                        "$project": {
                            "_id": 0,
                            "id": "$postgresql_id",
                            "client_id": 1,
                            "contract_number": 1,
                            "contract_type": 1,
                            "selected_address_type": 1,
                            "selected_address_id": 1,
                        }
                    },
                ],
                "as": "contract",
            }
        },
        {"$unwind": {"path": "$contract", "preserveNullAndEmptyArrays": True}},
        {
            "$lookup": {
                "from": "m_clients",
                "let": {"client_id": "$contract.client_id"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$client_id"]}}},
                    {"$limit": 1},
                    {
                        "$project": {
                            "_id": 0,
                            "id": "$postgresql_id",
                            "name": 1,
                        }
                    },
                ],
                "as": "client",
            }
        },
        {"$unwind": {"path": "$client", "preserveNullAndEmptyArrays": True}},
        {
            "$lookup": {
                "from": "m_building_alternate_addresses",
                "let": {"alt_addr_id": "$contract.selected_address_id"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$alt_addr_id"]}}},
                    {"$limit": 1},
                    {
                        "$project": {
                            "_id": 0,
                            "address": 1,
                        }
                    },
                ],
                "as": "alt_address",
            }
        },
        {"$unwind": {"path": "$alt_address", "preserveNullAndEmptyArrays": True}},
        {
            "$set": {
                "alternate_address": {
                    "$cond": [
                        {"$eq": ["$contract.selected_address_type", "Premises Address"]},
                        "$building.premises_address",
                        {"$ifNull": ["$alt_address.address", None]},
                    ]
                }
            }
        },
    ])

    pipeline.extend(
        [
            {
                "$lookup": {
                    "from": "me_report_inspections_group",
                    "let": {"inspection_id": "$_id"},
                    "pipeline": [
                        {"$match": {"$expr": {"$in": ["$$inspection_id", "$inspections"]}}},
                        {"$project": {"_id": 1}},
                        {"$limit": 1},
                    ],
                    "as": "assembled_report",
                }
            },
            {
                "$unwind": {
                    "path": "$assembled_report",
                    "preserveNullAndEmptyArrays": True,
                }
            },
            {"$set": {"assembled_report_id": {"$ifNull": ["$assembled_report._id", None]}}},
            {
                "$group": {
                    "_id": {"$ifNull": ["$assembled_report_id", "$_id"]},
                    "building_id": {"$first": "$building.id"},
                    "building_name": {"$first": "$building.name"},
                    "alternate_address": {"$first": "$alternate_address"},
                    "contract_number": {"$first": "$contract.contract_number"},
                    "contract_type": {"$first": "$contract.contract_type"},
                    "client_id": {"$first": "$client.id"},
                    "client_name": {"$first": "$client.name"},
                    "inspection_year": {"$first": "$inspection_year"},
                    "inspector": {"$first": "$inspector"},
                    "witnessing_inspector": {"$first": "$witnessing_inspector"},
                    "checklist_id": {"$first": "$checklist_id"},
                    "checklist_type": {"$first": "$checklist_type"},
                    "status": {"$first": "$status"},
                    "devices": {
                        "$push": {
                            "id": "$device_id",
                            "name": "$device.name",
                            "machine_number": "$device.machine_number",
                            "car_number": "$device.car_number",
                            "inspection_id": {"$toString": "$_id"},
                        }
                    },
                    "device": {
                        "$first": {
                            "$cond": [
                                {"$eq": ["$assembled_report_id", None]},
                                "$device",
                                None,
                            ]
                        }
                    },
                }
            },
            {"$sort": {"_id": -1}},
            {
                "$lookup": {
                    "from": "me_pages",
                    "let": {"inspections_group_id": "$_id"},
                    "pipeline": [
                        {"$match": {"$expr": {"$eq": ["$inspections_group_id", "$$inspections_group_id"]}}},
                        {"$limit": 1},
                    ],
                    "as": "me_page",
                }
            },
            {"$unwind": {"path": "$me_page", "preserveNullAndEmptyArrays": True}},
            {
                "$lookup": {
                    "from": "me_page_versions",
                    "let": {"current_version_id": "$me_page.current_version"},
                    "pipeline": [
                        {"$match": {"$expr": {"$eq": ["$_id", "$$current_version_id"]}}},
                        {"$limit": 1},
                        {
                            "$project": {
                                "_id": 0,
                                "id": {"$toString": "$_id"},
                                "name": "$name",
                                "version_number": "$version_number",
                            }
                        },
                    ],
                    "as": "current_version",
                }
            },
            {"$unwind": {"path": "$current_version", "preserveNullAndEmptyArrays": True}},
            {
                "$project": {
                    "_id": 0,
                    "id": {"$toString": "$_id"},
                    "row_type": {
                        "$cond": [{"$eq": ["$device", None]}, "group", "inspection"]
                    },
                    "building_id": "$building_id",
                    "building_name": "$building_name",
                    "alternate_address": "$alternate_address",
                    "contract_number": "$contract_number",
                    "contract_type": "$contract_type",
                    "client_id": "$client_id",
                    "client_name": "$client_name",
                    "checklist_id": "$checklist_id",
                    "checklist_type": "$checklist_type",
                    "inspection_year": "$inspection_year",
                    "status": "$status",
                    "inspector_id": {"$ifNull": ["$inspector.id", None]},
                    "inspector_name": {
                        "$trim": {
                            "input": {
                                "$concat": [
                                    "$inspector.first_name",
                                    " ",
                                    "$inspector.last_name",
                                ]
                            }
                        }
                    },
                    "witnessing_inspector_id": {"$ifNull": ["$witnessing_inspector.id", None]},
                    "witnessing_inspector_name": {
                        "$trim": {
                            "input": {
                                "$concat": [
                                    "$witnessing_inspector.first_name",
                                    " ",
                                    "$witnessing_inspector.last_name",
                                ]
                            }
                        }
                    },
                    "device": "$device",
                    "devices": "$devices",
                    "current_version": "$current_version",
                }
            },
        ]
    )

    if statuses:
        pipeline.append({"$match": {"status": {"$in": statuses}}})

    if devices:
        pipeline.append({"$match": {"devices.id": {"$in": list(map(int, devices))}}})

    if inspectors:
        sanitize_inspectors = list(map(int, inspectors))
        pipeline.append(
            {
                "$match": {
                    "$or": [
                        {"inspector_id": {"$in": sanitize_inspectors}},
                        {"witnessing_inspector_id": {"$in": sanitize_inspectors}},
                    ]
                }
            }
        )

    if inspection_year:
        pipeline.append({"$match": {"inspection_year": {"$in": list(map(int, inspection_year))}}})

    if search:
        search_regex = {"$regex": search, "$options": "i"}
        pipeline.append(
            {
                "$match": {
                    "$or": [
                        {"alternate_address": search_regex},
                        {"client_name": search_regex},
                        {"devices.name": search_regex},
                    ]
                }
            }
        )

    if checklist_id:
        pipeline.append({"$match": {"checklist_id": {"$in": list(map(int, checklist_id))}}})

    pipeline.extend(
        [
            {
                "$facet": {
                    "results": [{"$sort": {sort: order}}, {"$skip": offset}, {"$limit": limit}],
                    "count": [{"$count": "total"}],
                }
            },
            {"$project": {"results": 1, "count": {"$arrayElemAt": ["$count.total", 0]}}},
        ]
    )

    return pipeline


def get_maintenance_evaluations_filters_pipeline(params: dict):
    building_id = params.get("building_id")
    user_groups = params.get("user_groups")

    pipeline = [
        {
            "$project": {
                "_id": 1,
                "inspectionGroupId": 1,
                "device_id": 1,
                "status": 1,
                "inspection_date": 1,
                "inspection_year": {"$year": "$inspection_date"},
                "checklist_type": {"$first": "$inspection_checklists.type"},
            }
        },
        {"$match": {"checklist_type": 4}},
        {
            "$lookup": {
                "from": "inspection_groups",
                "let": {"inspections_group_id": "$inspectionGroupId"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$_id", "$$inspections_group_id"]}}},
                    {
                        "$project": {
                            "_id": 1,
                            "groupId": 1,
                            "buildingId": 1,
                            "inspectorId": 1,
                            "clientId": 1,
                            "witnessingInspectorId": 1,
                        }
                    },
                    {"$limit": 1},
                ],
                "as": "inspections_group",
            }
        },
        {
            "$unwind": {
                "path": "$inspections_group",
                "preserveNullAndEmptyArrays": True,
            }
        },
    ]

    if building_id:
        pipeline.append({"$match": {"inspections_group.buildingId": building_id}})
    else:
        pipeline.append({"$match": {"status": {"$in": ["Results Reviewed", "Assembling Report"]}}})

    pipeline.extend([
        {"$match": {"inspections_group.groupId": {"$in": user_groups}}},
        {
            "$lookup": {
                "from": "m_devices",
                "let": {"device_id": "$device_id"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$device_id"]}}},
                    {"$project": {"_id": 0, "id": "$postgresql_id", "name": 1}},
                    {"$limit": 1},
                ],
                "as": "device",
            }
        },
        {"$unwind": {"path": "$device", "preserveNullAndEmptyArrays": True}},
        {
            "$lookup": {
                "from": "m_clients",
                "let": {"client_id": "$inspections_group.clientId"},
                "pipeline": [
                    {"$match": {"$expr": {"$eq": ["$postgresql_id", "$$client_id"]}}},
                    {"$project": {"_id": 0, "id": "$postgresql_id", "name": 1}},
                    {"$limit": 1},
                ],
                "as": "client",
            }
        },
        {"$unwind": {"path": "$client", "preserveNullAndEmptyArrays": True}},
        {"$set": {"users_to_lookup": ["$inspections_group.inspectorId", "$inspections_group.witnessingInspectorId"]}},
        {
            "$lookup": {
                "from": "m_users",
                "let": {"ids": "$users_to_lookup"},
                "pipeline": [
                    {"$match": {"$expr": {"$in": ["$postgresql_id", "$$ids"]}}},
                    {"$project": {"_id": 0, "id": "$postgresql_id", "first_name": 1, "last_name": 1}},
                ],
                "as": "users",
            }
        },
        {
            "$set": {
                "inspector": {
                    "$first": {
                        "$filter": {
                            "input": "$users",
                            "as": "u",
                            "cond": {"$eq": ["$$u.id", "$inspections_group.inspectorId"]},
                        }
                    }
                },
                "witnessing_inspector": {
                    "$first": {
                        "$filter": {
                            "input": "$users",
                            "as": "u",
                            "cond": {"$eq": ["$$u.id", "$inspections_group.witnessingInspectorId"]},
                        }
                    }
                },
            }
        },
        {
            "$project": {
                "_id": 0,
                "device_id": "$device.id",
                "device_name": "$device.name",
                "status": "$status",
                "inspection_year": "$inspection_year",
                "client_id": "$client.id",
                "client_name": "$client.name",
                "inspector_id": {"$ifNull": ["$inspector.id", None]},
                "inspector_name": {
                    "$trim": {
                        "input": {
                            "$concat": [
                                {"$ifNull": ["$inspector.first_name", ""]},
                                " ",
                                {"$ifNull": ["$inspector.last_name", ""]},
                            ]
                        }
                    }
                },
                "witnessing_inspector_id": {"$ifNull": ["$witnessing_inspector.id", None]},
                "witnessing_inspector_name": {
                    "$trim": {
                        "input": {
                            "$concat": [
                                {"$ifNull": ["$witnessing_inspector.first_name", ""]},
                                " ",
                                {"$ifNull": ["$witnessing_inspector.last_name", ""]},
                            ]
                        }
                    }
                },
            }
        },
        {
            "$facet": {
                "devices": [
                    {"$match": {"device_id": {"$ne": None}, "device_name": {"$ne": None}, "device_name": {"$ne": ""}}},
                    {"$group": {"_id": "$device_id", "text": {"$first": "$device_name"}}},
                    {"$project": {"_id": 0, "value": "$_id", "text": 1}},
                    {"$sort": {"text": 1}},
                ],
                "clients": [
                    {"$match": {"client_id": {"$ne": None}, "client_name": {"$ne": None}, "client_name": {"$ne": ""}}},
                    {"$group": {"_id": "$client_id", "text": {"$first": "$client_name"}}},
                    {"$project": {"_id": 0, "value": "$_id", "text": 1}},
                    {"$sort": {"text": 1}},
                ],
                "statuses": [
                    {"$match": {"status": {"$ne": None}, "status": {"$ne": ""}}},
                    {"$group": {"_id": "$status"}},
                    {"$project": {"_id": 0, "value": "$_id", "text": "$_id"}},
                    {"$sort": {"text": 1}},
                ],
                "inspection_years": [
                    {"$match": {"inspection_year": {"$ne": None}}},
                    {"$group": {"_id": "$inspection_year"}},
                    {"$project": {"_id": 0, "value": "$_id", "text": {"$toString": "$_id"}}},
                    {"$sort": {"value": -1}},
                ],
                "inspectors": [
                    {
                        "$match": {
                            "$or": [
                                {"inspector_id": {"$ne": None}},
                                {"witnessing_inspector_id": {"$ne": None}},
                            ]
                        }
                    },
                    {
                        "$project": {
                            "inspector_options": [
                                {"id": "$inspector_id", "name": "$inspector_name"},
                                {"id": "$witnessing_inspector_id", "name": "$witnessing_inspector_name"},
                            ]
                        }
                    },
                    {"$unwind": "$inspector_options"},
                    {"$match": {"inspector_options.id": {"$nin": [None, "", 0]}}},
                    {"$group": {"_id": "$inspector_options.id", "text": {"$first": "$inspector_options.name"}}},
                    {"$project": {"_id": 0, "value": "$_id", "text": 1}},
                    {"$sort": {"text": 1}},
                ],
            }
        },
    ])

    return pipeline
