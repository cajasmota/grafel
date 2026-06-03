from bson import ObjectId
from django.db.models import Count, Q
from core.models.checklist import Checklist
from core.models.contract import Contract
from core.mongodb_collections import INSPECTIONS, INSPECTIONS_GROUP, ME_REPORT_INSPECTIONS_GROUP
from core.helper.mongo_helper import MongoDBConnection
from core.services.building.queries import (
    get_inspection_devices_pipeline,
    get_inspection_devices_filters_pipeline,
    get_maintenance_evaluations_pipeline,
    get_maintenance_evaluations_filters_pipeline,
)


def get_inspection_devices(params: dict):
    inspections_cln = MongoDBConnection.get_collection(INSPECTIONS)
    data = inspections_cln.aggregate(get_inspection_devices_pipeline(params)).next()

    if not data:
        return None

    return data


def get_inspection_devices_filters(params: dict):
    inspections_cln = MongoDBConnection.get_collection(INSPECTIONS)
    data = inspections_cln.aggregate(get_inspection_devices_filters_pipeline(params)).next()

    if not data:
        return None

    return data


def get_maintenance_evaluations(params: dict):
    inspections_cln = MongoDBConnection.get_collection(INSPECTIONS)
    data = inspections_cln.aggregate(get_maintenance_evaluations_pipeline(params)).next()
    
    if not data:
        return None

    try:
        results = data.get("results", [])
        for result in results:
            checklist_id = result.get("checklist_id")
            checklist = Checklist.objects.filter(id=checklist_id).first()
            result["checklist_name"] = checklist.name if checklist else None
    except Exception:
        pass

    return data


def get_maintenance_evaluations_filters(params: dict):
    inspections_cln = MongoDBConnection.get_collection(INSPECTIONS)
    data = inspections_cln.aggregate(get_maintenance_evaluations_filters_pipeline(params)).next()

    if not data:
        return None

    return data


def update_maintenance_evaluations(data: dict):
    inspections_cln = MongoDBConnection.get_collection(INSPECTIONS)
    me_report_group_cln = MongoDBConnection.get_collection(ME_REPORT_INSPECTIONS_GROUP)

    source_id = ObjectId(data.get("source_id"))
    destination_raw_id = data.get("destination_id")
    destination_id = ObjectId(destination_raw_id) if destination_raw_id != "new" else None
    inspections_ids = [ObjectId(inspection_id) for inspection_id in data.get("inspections")]

    try:
        pull_result = me_report_group_cln.update_one(
            {"_id": source_id},
            {"$pull": {"inspections": {"$in": inspections_ids}}},
        )

        if pull_result.modified_count <= 0:
            raise Exception("No inspections were removed from source")

        if destination_id is None:
            inspections_cln.update_many(
                {"_id": {"$in": inspections_ids}},
                {"$set": {"status": "Results Reviewed"}},
            )

            source_doc = me_report_group_cln.find_one({"_id": source_id})
            if source_doc and len(source_doc.get("inspections", [])) == 0:
                me_report_group_cln.delete_one({"_id": source_id})

            return pull_result

        push_result = me_report_group_cln.update_one(
            {"_id": destination_id},
            {"$addToSet": {"inspections": {"$each": inspections_ids}}},
        )

        if push_result.modified_count <= 0:
            me_report_group_cln.update_one(
                {"_id": source_id},
                {"$addToSet": {"inspections": {"$each": inspections_ids}}},
            )
            raise Exception("No inspections were added to destination")

        source_doc = me_report_group_cln.find_one({"_id": source_id})
        if source_doc and len(source_doc.get("inspections", [])) == 0:
            me_report_group_cln.delete_one({"_id": source_id})

        return push_result

    except Exception as e:
        raise Exception(f"There was an error updating the inspections: {str(e)}")


def delete_maintenance_evaluations(data: dict):
    inspections_cln = MongoDBConnection.get_collection(INSPECTIONS)
    me_report_group_cln = MongoDBConnection.get_collection(ME_REPORT_INSPECTIONS_GROUP)

    inspections_group_id = data.get("inspections_group_id")

    if not inspections_group_id:
        raise ValueError("inspections_group_id is required")

    try:
        source_group_id = ObjectId(inspections_group_id)

        me_report_group = me_report_group_cln.find_one({"_id": source_group_id})
        if not me_report_group:
            raise ValueError(f"ME report inspections group {inspections_group_id} not found")

        inspections_ids = me_report_group.get("inspections", [])

        if inspections_ids:
            inspections_cln.update_many(
                {"_id": {"$in": inspections_ids}},
                {"$set": {"status": "Results Reviewed"}},
            )

        deleted_result = me_report_group_cln.delete_one({"_id": source_group_id})
        if deleted_result.deleted_count <= 0:
            raise Exception("No ME report inspections group was deleted")

        return deleted_result

    except Exception as e:
        raise Exception(f"There was an error deleting the inspections group: {str(e)}")


def merge_maintenance_evaluations(data: dict):
    inspections_cln = MongoDBConnection.get_collection(INSPECTIONS)
    inspections_group_cln = MongoDBConnection.get_collection(INSPECTIONS_GROUP)
    me_report_group_cln = MongoDBConnection.get_collection(ME_REPORT_INSPECTIONS_GROUP)

    inspections = data.get("inspections")
    if not inspections:
        raise ValueError("No inspections provided")

    inspections_ids = [ObjectId(inspection["id"]) for inspection in inspections]

    first_inspection_doc = inspections_cln.find_one({"_id": inspections_ids[0]})
    if not first_inspection_doc:
        raise ValueError(f"Inspection {inspections_ids[0]} not found")

    inspections_group = inspections_group_cln.find_one({"_id": first_inspection_doc["inspectionGroupId"]})
    if not inspections_group:
        raise ValueError("Inspections group not found for the selected inspection")

    me_report_doc = {
        "building_id": inspections_group.get("buildingId"),
        "client_id": inspections_group.get("clientId"),
        "contract_id": inspections_group.get("contractId"),
        "group_id": inspections_group.get("groupId"),
        "jurisdiction_id": inspections_group.get("jurisdictionId"),
        "inspections": inspections_ids,
    }

    insert_result = me_report_group_cln.insert_one(me_report_doc)
    if not insert_result.inserted_id:
        raise Exception("Failed to create ME report inspections group")

    try:
        updated_result = inspections_cln.update_many(
            {"_id": {"$in": inspections_ids}},
            {"$set": {"status": "Assembling Report"}},
        )

        if updated_result.modified_count <= 0:
            me_report_group_cln.delete_one({"_id": insert_result.inserted_id})
            raise Exception("No inspections were updated")

        return str(insert_result.inserted_id)

    except Exception:
        me_report_group_cln.delete_one({"_id": insert_result.inserted_id})
        raise Exception("There was an error updating the inspections.")


def get_building_contract_conunters(params: dict):
    contracts = Contract.objects.filter(
        building_id=params.get("building_id"),
        group_id=params.get("group_id"),
    )

    counters = contracts.aggregate(
        all=Count("id"),
        active=Count("id", filter=Q(status="active")),
        proposal=Count("id", filter=Q(status="proposal")),
        expired=Count("id", filter=Q(status="expired")),
        canceled=Count("id", filter=Q(status__in=["cancelled", "canceled"])),
    )

    return {
        "all": counters.get("all", 0),
        "active": counters.get("active", 0),
        "proposal": counters.get("proposal", 0),
        "expired": counters.get("expired", 0),
        "canceled": counters.get("canceled", 0),
    }


def get_building_contract_filters(params: dict):
    contracts = Contract.objects.filter(
        building_id=params.get("building_id"),
        group_id=params.get("group_id"),
    )

    contract_filters = [
        {"text": contract.get("contract_number"), "value": contract.get("id")}
        for contract in contracts.exclude(contract_number__isnull=True)
        .exclude(contract_number="")
        .values("id", "contract_number")
        .order_by("contract_number")
    ]

    status_filters = [
        {"text": contract_status, "value": contract_status}
        for contract_status in contracts.exclude(status__isnull=True)
        .exclude(status="")
        .values_list("status", flat=True)
        .distinct()
        .order_by("status")
    ]

    return {
        "contracts": contract_filters,
        "statuses": status_filters,
    }
