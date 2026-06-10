"""
Tests for the MASS Scheduled Inspection CSV Import feature.

Covers:
  1. Missing required columns
  2. Non-MASS group rejection
  3. Dry run with unknown inspectors
  4. Finalize with new inspector creation
  5. Device resolution by Device.name
  6. Contract/client derivation
  7. Duplicate row skipped (warning, not error)
  8. Grouping rows into a single inspection group
  9. Partial success when some rows fail and others succeed
"""
import io
import json
from datetime import date, datetime
from unittest.mock import MagicMock, patch

import pytz
from django.test import TestCase
from rest_framework.test import APIClient, APITestCase

from core.helper.schedule_import_helper import (
    InspectionGroupKey,
    group_rows,
    map_test_type_to_checklist_name,
    normalize_row,
    parse_csv_file,
    resolve_checklist,
    resolve_contract,
    resolve_device,
    strip_test_type_tag_suffix,
)
from core.models import (
    Building,
    Checklist,
    Client,
    Contract,
    ContractDevice,
    Device,
    Group,
    GroupJurisdiction,
    Jurisdiction,
    Role,
    User,
    UserGroup,
    UserGroupRole,
)

UTC = pytz.utc

# ---------------------------------------------------------------------------
# Fixtures helpers
# ---------------------------------------------------------------------------

def make_jurisdiction(name="Massachusetts"):
    return Jurisdiction.objects.create(
        name=name,
        description="Test jurisdiction",
        is_public=False,
    )


def make_group(name="Test Elevator Co", jurisdiction=None):
    group = Group.objects.create(name=name, type="1")
    if jurisdiction:
        GroupJurisdiction.objects.create(group=group, jurisdiction=jurisdiction)
    return group


def make_building(jurisdiction=None, physical_address="123 Main St"):
    return Building.objects.create(
        physical_address=physical_address,
        jurisdiction=jurisdiction,
    )


def make_client(group):
    return Client.objects.create(name="Test Client", group=group)


def make_contract(group, client, building, status="active"):
    return Contract.objects.create(
        group=group,
        client=client,
        building=building,
        status=status,
        contract_number="CTR-001",
        contract_type="Cat1",
    )


def make_device(building, name="ELV-100"):
    return Device.objects.create(building=building, name=name)


def make_contract_device(contract, device, status="active"):
    return ContractDevice.objects.create(contract=contract, device=device, status=status)


def make_inspector(group, first_name="Jane", last_name="Smith"):
    inspector_role, _ = Role.objects.get_or_create(name="Inspector", defaults={"description": "Inspector"})
    user = User.objects.create(
        username=f"{first_name.lower()}.{last_name.lower()}@test.com",
        email=f"{first_name.lower()}.{last_name.lower()}@test.com",
        first_name=first_name,
        last_name=last_name,
        status="active",
    )
    UserGroup.objects.create(user=user, group=group)
    UserGroupRole.objects.create(user=user, group=group, role=inspector_role)
    return user


def make_checklist(name="Cat1", jurisdiction=None, group=None):
    from core.models import ChecklistType
    ctype, _ = ChecklistType.objects.get_or_create(name=name, defaults={"description": name})
    return Checklist.objects.create(
        name=name,
        description=name,
        type=ctype,
        jurisdiction=jurisdiction,
        group=group,
        status="active",
        active=True,
    )


def make_csv(*rows, headers=None):
    """Build CSV bytes from a list of row dicts."""
    if headers is None and rows:
        headers = list(rows[0].keys())
    buf = io.StringIO()
    import csv as _csv
    writer = _csv.DictWriter(buf, fieldnames=headers)
    writer.writeheader()
    for row in rows:
        writer.writerow(row)
    return buf.getvalue().encode("utf-8")


# ---------------------------------------------------------------------------
# 1. Missing required columns
# ---------------------------------------------------------------------------

class ParseCsvFileMissingColumnsTest(TestCase):

    def test_missing_test_type_column(self):
        csv_bytes = make_csv(
            {"Date": "05/01/2026", "Time": "09:00", "Inspector": "Jane Smith", "Tag# / Device ID": "ELV-100"},
        )
        rows, errors = parse_csv_file(csv_bytes)
        self.assertEqual(rows, [])
        self.assertEqual(len(errors), 1)
        self.assertEqual(errors[0]["code"], "MISSING_COLUMNS")
        self.assertIn("test type", errors[0]["context"]["missing"])

    def test_missing_device_column(self):
        csv_bytes = make_csv(
            {"Date": "05/01/2026", "Time": "09:00", "Inspector": "Jane Smith", "Test Type": "Cat1"},
        )
        rows, errors = parse_csv_file(csv_bytes)
        self.assertEqual(rows, [])
        self.assertIn("tag# / device id", errors[0]["context"]["missing"])

    def test_empty_file(self):
        rows, errors = parse_csv_file(b"")
        self.assertEqual(rows, [])
        self.assertEqual(errors[0]["code"], "EMPTY_FILE")

    def test_valid_headers_returns_rows(self):
        csv_bytes = make_csv(
            {
                "Date": "05/01/2026",
                "Time": "09:00",
                "Inspector": "Jane Smith",
                "Tag# / Device ID": "ELV-100",
                "Test Type": "Cat1",
            }
        )
        rows, errors = parse_csv_file(csv_bytes)
        self.assertEqual(errors, [])
        self.assertEqual(len(rows), 1)


# ---------------------------------------------------------------------------
# 2. Non-MASS group rejection
# ---------------------------------------------------------------------------

class NonMassGroupRejectionTest(APITestCase):

    def setUp(self):
        # Group with NO jurisdiction
        self.non_mass_group = Group.objects.create(name="NYC Group", type="1")
        self.user = User.objects.create(
            username="admin@test.com", email="admin@test.com", status="active",
            is_staff=True,
        )
        UserGroup.objects.create(user=self.user, group=self.non_mass_group)
        self.client = APIClient()
        self.client.force_authenticate(user=self.user)

    def test_non_mass_group_returns_403(self):
        csv_bytes = make_csv(
            {
                "Date": "05/01/2026", "Time": "09:00", "Inspector": "Jane Smith",
                "Tag# / Device ID": "ELV-100", "Test Type": "Cat1",
            }
        )
        response = self.client.post(
            "/schedule/import/",
            data={
                "file": io.BytesIO(csv_bytes),
                "group_id": self.non_mass_group.id,
                "dry_run": "true",
            },
            format="multipart",
        )
        self.assertEqual(response.status_code, 403)
        self.assertEqual(response.data["code"], "NOT_MASS_JURISDICTION")


# ---------------------------------------------------------------------------
# 3. Dry run — unknown inspectors
# ---------------------------------------------------------------------------

class DryRunUnknownInspectorTest(APITestCase):

    def setUp(self):
        self.jurisdiction = make_jurisdiction()
        self.group = make_group(jurisdiction=self.jurisdiction)
        self.building = make_building(jurisdiction=self.jurisdiction)
        self.client_obj = make_client(self.group)
        self.contract = make_contract(self.group, self.client_obj, self.building)
        self.device = make_device(self.building)
        make_contract_device(self.contract, self.device)
        make_checklist("Cat1", jurisdiction=self.jurisdiction)

        self.user = User.objects.create(
            username="admin2@test.com", email="admin2@test.com", status="active",
        )
        UserGroup.objects.create(user=self.user, group=self.group)
        self.client = APIClient()
        self.client.force_authenticate(user=self.user)

    @patch("core.views.schedule_viewset.MongoDBConnection.get_collection")
    def test_dry_run_returns_unknown_inspector(self, mock_get_col):
        mock_col = MagicMock()
        mock_col.aggregate.return_value = []
        mock_get_col.return_value = mock_col

        csv_bytes = make_csv(
            {
                "Date": "05/01/2026", "Time": "09:00",
                "Inspector": "Unknown Person",
                "Tag# / Device ID": self.device.name,
                "Test Type": "Cat1",
            }
        )
        response = self.client.post(
            "/schedule/import/",
            data={
                "file": io.BytesIO(csv_bytes),
                "group_id": self.group.id,
                "dry_run": "true",
            },
            format="multipart",
        )
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.data["status"], "preview")
        self.assertIn("Unknown Person", response.data["unknown_inspectors"])
        row = response.data["row_results"][0]
        self.assertEqual(row["status"], "pending_inspector")


# ---------------------------------------------------------------------------
# 4. Finalize — new inspector creation
# ---------------------------------------------------------------------------

class FinalizeNewInspectorTest(APITestCase):

    def setUp(self):
        self.jurisdiction = make_jurisdiction()
        self.group = make_group(jurisdiction=self.jurisdiction)
        self.building = make_building(jurisdiction=self.jurisdiction)
        self.client_obj = make_client(self.group)
        self.contract = make_contract(self.group, self.client_obj, self.building)
        self.device = make_device(self.building, name="ELV-200")
        make_contract_device(self.contract, self.device)
        make_checklist("Cat1", jurisdiction=self.jurisdiction)

        self.user = User.objects.create(
            username="admin3@test.com", email="admin3@test.com", status="active",
        )
        UserGroup.objects.create(user=self.user, group=self.group)
        self.client = APIClient()
        self.client.force_authenticate(user=self.user)

    @patch("core.views.schedule_viewset.MongoDBConnection.get_collection")
    def test_finalize_creates_inspector_and_returns_201(self, mock_get_col):
        mock_ig_col = MagicMock()
        mock_ig_col.insert_one.return_value = MagicMock(inserted_id="fake_ig_id")
        mock_ig_col.aggregate.return_value = []

        mock_i_col = MagicMock()
        mock_i_col.bulk_write.return_value = MagicMock()

        def get_col_side_effect(name):
            if name == "inspection_groups":
                return mock_ig_col
            return mock_i_col

        mock_get_col.side_effect = get_col_side_effect

        csv_bytes = make_csv(
            {
                "Date": "05/01/2026", "Time": "09:00",
                "Inspector": "New Inspector",
                "Tag# / Device ID": "ELV-200",
                "Test Type": "Cat1",
            }
        )
        response = self.client.post(
            "/schedule/import/",
            data={
                "file": io.BytesIO(csv_bytes),
                "group_id": self.group.id,
                "dry_run": "false",
                "inspector_emails": json.dumps({"New Inspector": "newinspector@test.com"}),
            },
            format="multipart",
        )
        self.assertEqual(response.status_code, 201)
        self.assertEqual(len(response.data["created_inspectors"]), 1)
        self.assertEqual(response.data["created_inspectors"][0]["email"], "newinspector@test.com")
        self.assertEqual(response.data["imported_count"], 1)

        # Verify user was created in the DB
        new_user = User.objects.get(email="newinspector@test.com")
        self.assertEqual(new_user.first_name, "New")
        self.assertEqual(new_user.last_name, "Inspector")
        # Verify role linkage
        self.assertTrue(
            UserGroupRole.objects.filter(user=new_user, group=self.group, role__name="Inspector").exists()
        )


# ---------------------------------------------------------------------------
# 5. Device resolution by Device.name
# ---------------------------------------------------------------------------

class ResolveDeviceTest(TestCase):

    def setUp(self):
        self.jurisdiction = make_jurisdiction()
        self.group = make_group(jurisdiction=self.jurisdiction)
        self.building = make_building(jurisdiction=self.jurisdiction)
        self.client_obj = make_client(self.group)
        self.contract = make_contract(self.group, self.client_obj, self.building)
        self.device = make_device(self.building, name="ELV-300")
        make_contract_device(self.contract, self.device)

    def test_matches_by_name_exact(self):
        device, errors = resolve_device("ELV-300", self.group.id)
        self.assertIsNotNone(device)
        self.assertEqual(device.name, "ELV-300")
        self.assertEqual(errors, [])

    def test_matches_case_insensitive(self):
        device, errors = resolve_device("elv-300", self.group.id)
        self.assertIsNotNone(device)
        self.assertEqual(errors, [])

    def test_not_found_returns_error(self):
        device, errors = resolve_device("NONEXISTENT", self.group.id)
        self.assertIsNone(device)
        self.assertEqual(errors[0]["code"], "DEVICE_NOT_FOUND")

    def test_wrong_group_still_finds_device_by_name(self):
        # resolve_device no longer filters by group in the primary query.
        # The device is found; group/contract validation is left to resolve_contract().
        other_group = make_group("Other Group")
        device, errors = resolve_device("ELV-300", other_group.id)
        self.assertIsNotNone(device)
        self.assertEqual(errors, [])

    def test_inactive_contract_device_still_found_by_name(self):
        # Contract status does not block the primary device lookup.
        # resolve_contract() will return NO_ACTIVE_CONTRACT for the caller to handle.
        self.device.device_contracts.update(status="inactive")
        device, errors = resolve_device("ELV-300", self.group.id)
        self.assertIsNotNone(device)
        self.assertEqual(errors, [])


# ---------------------------------------------------------------------------
# 6. Contract / client derivation
# ---------------------------------------------------------------------------

class ResolveContractTest(TestCase):

    def setUp(self):
        self.jurisdiction = make_jurisdiction()
        self.group = make_group(jurisdiction=self.jurisdiction)
        self.building = make_building(jurisdiction=self.jurisdiction)
        self.client_obj = make_client(self.group)
        self.contract = make_contract(self.group, self.client_obj, self.building)
        self.device = make_device(self.building, name="ELV-400")
        self.cd = make_contract_device(self.contract, self.device)

    def test_returns_contract_device_with_client(self):
        cd, errors, warnings = resolve_contract(self.device.id, self.group.id)
        self.assertIsNotNone(cd)
        self.assertEqual(errors, [])
        self.assertEqual(cd.contract.client.name, "Test Client")

    def test_inactive_contract_returns_error(self):
        self.contract.status = "inactive"
        self.contract.save()
        cd, errors, warnings = resolve_contract(self.device.id, self.group.id)
        self.assertIsNone(cd)
        self.assertEqual(errors[0]["code"], "NO_ACTIVE_CONTRACT")

    def test_wrong_group_returns_error(self):
        other_group = make_group("Other Group 2")
        cd, errors, warnings = resolve_contract(self.device.id, other_group.id)
        self.assertIsNone(cd)
        self.assertEqual(errors[0]["code"], "NO_ACTIVE_CONTRACT")


# ---------------------------------------------------------------------------
# 7. Duplicate row — skipped with warning, not error
# ---------------------------------------------------------------------------

class DuplicateRowSkippedTest(APITestCase):

    def setUp(self):
        self.jurisdiction = make_jurisdiction()
        self.group = make_group(jurisdiction=self.jurisdiction)
        self.building = make_building(jurisdiction=self.jurisdiction)
        self.client_obj = make_client(self.group)
        self.contract = make_contract(self.group, self.client_obj, self.building)
        self.device = make_device(self.building, name="ELV-500")
        make_contract_device(self.contract, self.device)
        self.checklist = make_checklist("Cat1", jurisdiction=self.jurisdiction)
        self.inspector = make_inspector(self.group)

        self.user = User.objects.create(
            username="admin4@test.com", email="admin4@test.com", status="active",
        )
        UserGroup.objects.create(user=self.user, group=self.group)
        self.client = APIClient()
        self.client.force_authenticate(user=self.user)

    @patch("core.views.schedule_viewset.validate_existing_inspection")
    @patch("core.views.schedule_viewset.MongoDBConnection.get_collection")
    def test_duplicate_row_is_skipped_not_errored(self, mock_get_col, mock_validate):
        mock_col = MagicMock()
        mock_col.aggregate.return_value = []
        mock_get_col.return_value = mock_col

        # Simulate duplicate detected
        mock_validate.return_value = (False, "An inspection of this type already exists within the specified range.")

        csv_bytes = make_csv(
            {
                "Date": "05/01/2026", "Time": "09:00",
                "Inspector": f"{self.inspector.first_name} {self.inspector.last_name}",
                "Tag# / Device ID": "ELV-500",
                "Test Type": "Cat1",
            }
        )
        response = self.client.post(
            "/schedule/import/",
            data={
                "file": io.BytesIO(csv_bytes),
                "group_id": self.group.id,
                "dry_run": "true",
            },
            format="multipart",
        )
        self.assertEqual(response.status_code, 200)
        row = response.data["row_results"][0]
        self.assertEqual(row["status"], "skipped")
        warning_codes = [w["code"] for w in row["warnings"]]
        self.assertIn("DUPLICATE_INSPECTION_SKIPPED", warning_codes)


# ---------------------------------------------------------------------------
# 8. Row grouping — same date + inspector + building → one group
# ---------------------------------------------------------------------------

class GroupRowsTest(TestCase):

    def _make_resolved_row(self, row_index, start_date_iso, inspector_id, building_id):
        return {
            "row_index": row_index,
            "raw_inspector": "Jane Smith",
            "raw_tag_number": f"ELV-{row_index}",
            "raw_address": "",
            "raw_company": "",
            "resolved": {
                "device_id": row_index,
                "device_name": f"ELV-{row_index}",
                "building_id": building_id,
                "building_name": "Test Building",
                "building_address": "123 Main St",
                "contract_id": 1,
                "contract_number": "CTR-001",
                "contract_type": "Cat1",
                "client_id": 1,
                "client_name": "Test Client",
                "jurisdiction_id": 1,
                "inspector_id": inspector_id,
                "inspector_name": "Jane Smith",
                "checklist_id": 1,
                "checklist_type_int": 1,
                "checklist_name": "Cat1",
                "start_date_iso": start_date_iso,
                "start_time_str": "09:00:00",
                "_start_dt": datetime(2026, 5, 1, 9, 0, 0, tzinfo=UTC),
                "_end_dt": datetime(2026, 5, 1, 0, 0, 0, tzinfo=UTC),
            },
            "errors": [],
            "warnings": [],
            "status": "valid",
        }

    def test_two_devices_same_date_inspector_building_are_one_group(self):
        rows = [
            self._make_resolved_row(1, "2026-05-01", inspector_id=10, building_id=5),
            self._make_resolved_row(2, "2026-05-01", inspector_id=10, building_id=5),
        ]
        grouped = group_rows(rows)
        self.assertEqual(len(grouped), 1)
        key = list(grouped.keys())[0]
        self.assertEqual(len(grouped[key]), 2)

    def test_different_buildings_produce_separate_groups(self):
        rows = [
            self._make_resolved_row(1, "2026-05-01", inspector_id=10, building_id=5),
            self._make_resolved_row(2, "2026-05-01", inspector_id=10, building_id=6),
        ]
        grouped = group_rows(rows)
        self.assertEqual(len(grouped), 2)

    def test_different_dates_produce_separate_groups(self):
        rows = [
            self._make_resolved_row(1, "2026-05-01", inspector_id=10, building_id=5),
            self._make_resolved_row(2, "2026-05-02", inspector_id=10, building_id=5),
        ]
        grouped = group_rows(rows)
        self.assertEqual(len(grouped), 2)

    def test_different_inspectors_produce_separate_groups(self):
        rows = [
            self._make_resolved_row(1, "2026-05-01", inspector_id=10, building_id=5),
            self._make_resolved_row(2, "2026-05-01", inspector_id=11, building_id=5),
        ]
        grouped = group_rows(rows)
        self.assertEqual(len(grouped), 2)


# ---------------------------------------------------------------------------
# 9. Partial success — some rows fail, others succeed
# ---------------------------------------------------------------------------

class PartialSuccessTest(APITestCase):

    def setUp(self):
        self.jurisdiction = make_jurisdiction()
        self.group = make_group(jurisdiction=self.jurisdiction)
        self.building = make_building(jurisdiction=self.jurisdiction)
        self.client_obj = make_client(self.group)
        self.contract = make_contract(self.group, self.client_obj, self.building)
        self.device = make_device(self.building, name="ELV-600")
        make_contract_device(self.contract, self.device)
        make_checklist("Cat1", jurisdiction=self.jurisdiction)
        self.inspector = make_inspector(self.group, first_name="John", last_name="Doe")

        self.user = User.objects.create(
            username="admin5@test.com", email="admin5@test.com", status="active",
        )
        UserGroup.objects.create(user=self.user, group=self.group)
        self.client = APIClient()
        self.client.force_authenticate(user=self.user)

    @patch("core.views.schedule_viewset.validate_existing_inspection")
    @patch("core.views.schedule_viewset.MongoDBConnection.get_collection")
    def test_one_valid_one_bad_device_returns_partial(self, mock_get_col, mock_validate):
        mock_ig_col = MagicMock()
        mock_ig_col.insert_one.return_value = MagicMock(inserted_id="fake_id")
        mock_ig_col.aggregate.return_value = []
        mock_i_col = MagicMock()
        mock_i_col.bulk_write.return_value = MagicMock()

        def get_col_side_effect(name):
            if name == "inspection_groups":
                return mock_ig_col
            return mock_i_col

        mock_get_col.side_effect = get_col_side_effect
        mock_validate.return_value = (True, "Inspection can be scheduled.")

        csv_bytes = make_csv(
            # Row 1: valid
            {
                "Date": "05/01/2026", "Time": "09:00",
                "Inspector": "John Doe",
                "Tag# / Device ID": "ELV-600",
                "Test Type": "Cat1",
            },
            # Row 2: bad device
            {
                "Date": "05/01/2026", "Time": "10:00",
                "Inspector": "John Doe",
                "Tag# / Device ID": "NONEXISTENT",
                "Test Type": "Cat1",
            },
        )
        response = self.client.post(
            "/schedule/import/",
            data={
                "file": io.BytesIO(csv_bytes),
                "group_id": self.group.id,
                "dry_run": "false",
            },
            format="multipart",
        )
        self.assertEqual(response.status_code, 201)
        self.assertEqual(response.data["status"], "partial")
        self.assertEqual(response.data["imported_count"], 1)
        self.assertEqual(response.data["skipped_count"], 1)
        skipped_reasons = [r["reason"] for r in response.data["skipped_rows"]]
        self.assertIn("DEVICE_NOT_FOUND", skipped_reasons)


# ---------------------------------------------------------------------------
# 10. Production CSV format — headers with leading spaces, Elevator Company
# ---------------------------------------------------------------------------

class MassProductionCsvHeadersTest(TestCase):
    """
    Verify that headers with leading spaces (as produced by the MASS CSV
    export) parse correctly after _norm() whitespace trimming.
    """

    def test_leading_space_headers_are_accepted(self):
        # Headers exactly as received in production (spaces before most cols)
        csv_bytes = (
            b"Date, Time, Inspector, Tag #, Address, Test Type, Elevator Company\r\n"
            b"1/5/2026,8:00,Ronald Travers,282-P-12,18 DANA HILL ROAD,Annual Inspection - 282-P-12,Otis Elevator Company-RI\r\n"
        )
        rows, errors = parse_csv_file(csv_bytes)
        self.assertEqual(errors, [], msg=f"Unexpected top-level errors: {errors}")
        self.assertEqual(len(rows), 1)
        # Normalized keys must be present
        row = rows[0]
        self.assertIn("tag #", row)
        self.assertIn("test type", row)
        self.assertIn("elevator company", row)

    def test_elevator_company_maps_to_raw_company(self):
        """normalize_row() must populate raw_company from 'elevator company'."""
        row = {
            "date": "1/5/2026",
            "time": "8:00",
            "inspector": "Ronald Travers",
            "tag #": "282-P-12",
            "address": "18 DANA HILL ROAD, STERLING",
            "test type": "Annual Inspection - 282-P-12",
            "elevator company": "Otis Elevator Company-RI",
        }
        result = normalize_row(row, row_index=1)
        self.assertEqual(result["raw_company"], "Otis Elevator Company-RI")

    def test_blank_elevator_company_does_not_crash(self):
        row = {
            "date": "1/5/2026",
            "time": "8:00",
            "inspector": "Ronald Travers",
            "tag #": "282-P-12",
            "address": "",
            "test type": "Annual Inspection - 282-P-12",
            "elevator company": "",
        }
        result = normalize_row(row, row_index=1)
        self.assertEqual(result["raw_company"], "")
        # No error from blank optional fields
        error_codes = [e["code"] for e in result["errors"]]
        self.assertNotIn("MISSING_INSPECTOR", error_codes)


# ---------------------------------------------------------------------------
# 11. Test-type keyword mapping
# ---------------------------------------------------------------------------

class MapTestTypeTest(TestCase):
    """Unit tests for map_test_type_to_checklist_name()."""

    # --- "Annual Inspection" variants → Cat1 ---

    def test_annual_inspection_plain(self):
        name, unsupported = map_test_type_to_checklist_name("Annual Inspection")
        self.assertEqual(name, "Cat1")
        self.assertFalse(unsupported)

    def test_annual_inspection_with_tag_suffix(self):
        # Production value: "Annual Inspection - 282-P-12"
        name, unsupported = map_test_type_to_checklist_name("Annual Inspection - 282-P-12")
        self.assertEqual(name, "Cat1")
        self.assertFalse(unsupported)

    def test_annual_inspection_case_insensitive(self):
        name, unsupported = map_test_type_to_checklist_name("ANNUAL INSPECTION - ELV")
        self.assertEqual(name, "Cat1")
        self.assertFalse(unsupported)

    # --- "Annual Weight Test" variants → Cat5 ---

    def test_annual_weight_test_plain(self):
        name, unsupported = map_test_type_to_checklist_name("Annual Weight Test")
        self.assertEqual(name, "Cat5")
        self.assertFalse(unsupported)

    def test_annual_weight_test_with_tag_suffix(self):
        name, unsupported = map_test_type_to_checklist_name("Annual Weight Test - 282-P-12")
        self.assertEqual(name, "Cat5")
        self.assertFalse(unsupported)

    def test_annual_weight_test_not_swallowed_by_annual_inspection(self):
        # "annual weight test" contains "annual" but must NOT resolve to Cat1.
        name, _ = map_test_type_to_checklist_name("Annual Weight Test")
        self.assertEqual(name, "Cat5", msg="'annual weight test' must map to Cat5, not Cat1")

    # --- "90 Day Annual" → unsupported (skip, not error) ---

    def test_90_day_annual_is_unsupported(self):
        name, unsupported = map_test_type_to_checklist_name("90 Day Annual")
        self.assertIsNone(name)
        self.assertTrue(unsupported)

    def test_90_day_annual_with_suffix_is_unsupported(self):
        name, unsupported = map_test_type_to_checklist_name("90 Day Annual - 282-P-12")
        self.assertIsNone(name)
        self.assertTrue(unsupported)

    # --- Existing short-form values still resolve (backward compat) ---

    def test_exact_cat1_still_works(self):
        name, unsupported = map_test_type_to_checklist_name("Cat1")
        self.assertEqual(name, "Cat1")
        self.assertFalse(unsupported)

    def test_exact_five_year_still_works(self):
        name, unsupported = map_test_type_to_checklist_name("5 year")
        self.assertEqual(name, "Cat5")
        self.assertFalse(unsupported)

    def test_periodic_still_works(self):
        name, unsupported = map_test_type_to_checklist_name("Periodic")
        self.assertEqual(name, "Periodic")
        self.assertFalse(unsupported)

    # --- Completely unknown → error signal ---

    def test_unknown_returns_none_not_unsupported(self):
        name, unsupported = map_test_type_to_checklist_name("Biennial Safety Check")
        self.assertIsNone(name)
        self.assertFalse(unsupported)


# ---------------------------------------------------------------------------
# 12. normalize_row — production CSV values end-to-end
# ---------------------------------------------------------------------------

class NormalizeRowProductionValuesTest(TestCase):
    """
    normalize_row() behaviour with the exact field values from the
    production MASS CSV (already normalized to lowercase keys by
    parse_csv_file → _norm()).
    """

    def _base_row(self, **overrides):
        row = {
            "date": "1/5/2026",
            "time": "8:00",
            "inspector": "Ronald Travers",
            "tag #": "282-P-12",
            "address": "18 DANA HILL ROAD, STERLING",
            "test type": "Annual Inspection - 282-P-12",
            "elevator company": "Otis Elevator Company-RI",
        }
        row.update(overrides)
        return row

    def test_annual_inspection_resolves_to_cat1(self):
        result = normalize_row(self._base_row(), row_index=1)
        self.assertEqual(result["resolved"]["checklist_name"], "Cat1")
        self.assertEqual(result["status"], "valid")

    def test_annual_weight_test_resolves_to_cat5(self):
        result = normalize_row(
            self._base_row(**{"test type": "Annual Weight Test - 282-P-12"}),
            row_index=1,
        )
        self.assertEqual(result["resolved"]["checklist_name"], "Cat5")
        self.assertEqual(result["status"], "valid")

    def test_90_day_annual_is_skipped_not_errored(self):
        result = normalize_row(
            self._base_row(**{"test type": "90 Day Annual - 282-P-12"}),
            row_index=1,
        )
        self.assertEqual(result["status"], "skipped")
        warning_codes = [w["code"] for w in result["warnings"]]
        self.assertIn("UNSUPPORTED_TEST_TYPE", warning_codes)
        # Must NOT be in errors — other rows should not be blocked
        error_codes = [e["code"] for e in result["errors"]]
        self.assertNotIn("UNKNOWN_TEST_TYPE", error_codes)

    def test_unknown_test_type_is_error(self):
        result = normalize_row(
            self._base_row(**{"test type": "Completely Unknown Type XYZ"}),
            row_index=1,
        )
        self.assertEqual(result["status"], "error")
        error_codes = [e["code"] for e in result["errors"]]
        self.assertIn("UNKNOWN_TEST_TYPE", error_codes)

    def test_missing_inspector_is_error(self):
        result = normalize_row(self._base_row(inspector=""), row_index=1)
        self.assertEqual(result["status"], "error")
        error_codes = [e["code"] for e in result["errors"]]
        self.assertIn("MISSING_INSPECTOR", error_codes)

    def test_missing_tag_number_is_error(self):
        row = self._base_row()
        row["tag #"] = ""
        result = normalize_row(row, row_index=1)
        self.assertEqual(result["status"], "error")
        error_codes = [e["code"] for e in result["errors"]]
        self.assertIn("MISSING_TAG_NUMBER", error_codes)

    def test_blank_address_and_company_do_not_error(self):
        result = normalize_row(
            self._base_row(address="", **{"elevator company": ""}),
            row_index=1,
        )
        self.assertEqual(result["raw_address"], "")
        self.assertEqual(result["raw_company"], "")
        # Should still be valid (optional fields)
        self.assertEqual(result["status"], "valid")

    def test_date_parses_m_d_yyyy(self):
        """1/5/2026 (no zero-padding) must parse correctly."""
        result = normalize_row(self._base_row(), row_index=1)
        self.assertEqual(result["resolved"]["start_date_iso"], "2026-01-05")

    def test_time_parses_h_mm(self):
        """8:00 (no zero-padding on hour) must parse correctly."""
        result = normalize_row(self._base_row(), row_index=1)
        self.assertEqual(result["resolved"]["start_time_str"], "08:00:00")


# ---------------------------------------------------------------------------
# 13. resolve_device — new name-first lookup behaviour
# ---------------------------------------------------------------------------

class ResolveDeviceNameFirstTest(TestCase):
    """
    Verify that resolve_device finds devices by name without requiring an
    active contract, and uses the contract / address only for disambiguation
    when multiple devices share the same name.
    """

    def setUp(self):
        self.jurisdiction = make_jurisdiction()
        self.group = make_group(jurisdiction=self.jurisdiction)
        self.building = make_building(
            jurisdiction=self.jurisdiction,
            physical_address="18 DANA HILL ROAD, STERLING",
        )
        self.client_obj = make_client(self.group)
        self.contract = make_contract(self.group, self.client_obj, self.building)
        self.device = make_device(self.building, name="282-P-12")
        make_contract_device(self.contract, self.device)

    def test_finds_device_by_name_with_active_contract(self):
        device, errors = resolve_device("282-P-12", self.group.id)
        self.assertIsNotNone(device)
        self.assertEqual(device.name, "282-P-12")
        self.assertEqual(errors, [])

    def test_finds_device_even_when_contract_device_inactive(self):
        """Contract status must not block the primary name lookup."""
        self.device.device_contracts.update(status="inactive")
        device, errors = resolve_device("282-P-12", self.group.id)
        self.assertIsNotNone(device)
        self.assertEqual(errors, [])

    def test_finds_device_for_wrong_group(self):
        """Device IS returned; the group mismatch surfaces via resolve_contract."""
        other_group = make_group("Other Group MASS")
        device, errors = resolve_device("282-P-12", other_group.id)
        self.assertIsNotNone(device)
        self.assertEqual(errors, [])

    def test_not_found_returns_device_not_found(self):
        device, errors = resolve_device("NONEXISTENT-TAG", self.group.id)
        self.assertIsNone(device)
        self.assertEqual(errors[0]["code"], "DEVICE_NOT_FOUND")

    def test_whitespace_in_tag_is_trimmed(self):
        device, errors = resolve_device("  282-P-12  ", self.group.id)
        self.assertIsNotNone(device)
        self.assertEqual(errors, [])

    def test_case_insensitive_match(self):
        device, errors = resolve_device("282-p-12", self.group.id)
        self.assertIsNotNone(device)
        self.assertEqual(errors, [])

    def test_ambiguous_disambiguated_by_group_contract(self):
        """
        Two devices share the same name; the one linked to the uploading group's
        active contract should be returned.
        """
        other_building = make_building(
            jurisdiction=self.jurisdiction,
            physical_address="99 OTHER STREET, BOSTON",
        )
        other_group = make_group("Other Group MASS 2")
        other_client = make_client(other_group)
        other_contract = make_contract(other_group, other_client, other_building)
        other_device = make_device(other_building, name="282-P-12")
        make_contract_device(other_contract, other_device)

        device, errors = resolve_device("282-P-12", self.group.id)
        self.assertIsNotNone(device)
        self.assertEqual(device.id, self.device.id)
        self.assertEqual(errors, [])

    def test_ambiguous_disambiguated_by_address(self):
        """
        Two devices share the same name AND same group — address breaks the tie.
        """
        other_building = make_building(
            jurisdiction=self.jurisdiction,
            physical_address="99 OTHER STREET, BOSTON",
        )
        other_client = make_client(self.group)
        other_contract = make_contract(self.group, other_client, other_building)
        other_device = make_device(other_building, name="282-P-12")
        make_contract_device(other_contract, other_device)

        device, errors = resolve_device(
            "282-P-12", self.group.id,
            raw_address="18 DANA HILL ROAD, STERLING",
        )
        self.assertIsNotNone(device)
        self.assertEqual(device.id, self.device.id)
        self.assertEqual(errors, [])


# ---------------------------------------------------------------------------
# 14. resolve_checklist — type__name__iexact lookup
# ---------------------------------------------------------------------------

class ResolveChecklistByTypeTest(TestCase):
    """
    Verify that resolve_checklist finds checklists by their ChecklistType.name
    (e.g. 'CAT1', 'CAT5') rather than by the Checklist display name.
    This handles production DBs where checklists are named arbitrarily but
    their type is always the stable semantic identifier.
    """

    def setUp(self):
        self.jurisdiction = make_jurisdiction()
        self.group = make_group(jurisdiction=self.jurisdiction)

    def _make_checklist_with_type(self, type_name, checklist_name, jurisdiction=None):
        from core.models import ChecklistType
        ctype, _ = ChecklistType.objects.get_or_create(
            name=type_name, defaults={"description": type_name}
        )
        return Checklist.objects.create(
            name=checklist_name,
            description=checklist_name,
            type=ctype,
            jurisdiction=jurisdiction or self.jurisdiction,
            status="active",
            active=True,
        )

    def test_finds_checklist_when_type_name_is_uppercase_cat1(self):
        """ChecklistType.name='CAT1' must match canonical name 'Cat1' via iexact."""
        self._make_checklist_with_type("CAT1", "Annual Inspection Checklist")
        checklist, errors = resolve_checklist("Cat1", self.jurisdiction.id)
        self.assertIsNotNone(checklist)
        self.assertEqual(errors, [])

    def test_finds_checklist_when_type_name_is_uppercase_cat5(self):
        self._make_checklist_with_type("CAT5", "Weight Test Checklist")
        checklist, errors = resolve_checklist("Cat5", self.jurisdiction.id)
        self.assertIsNotNone(checklist)
        self.assertEqual(errors, [])

    def test_finds_checklist_with_custom_display_name(self):
        """Display name 'Cat1 ELV3 Checklist' should still resolve for type Cat1."""
        self._make_checklist_with_type("CAT1", "Cat1 ELV3 Checklist")
        checklist, errors = resolve_checklist("Cat1", self.jurisdiction.id)
        self.assertIsNotNone(checklist)
        self.assertEqual(errors, [])

    def test_returns_error_when_no_checklist_for_jurisdiction(self):
        """Correct error code when no checklist is configured for this jurisdiction."""
        other_jurisdiction = make_jurisdiction("New York")
        self._make_checklist_with_type("CAT1", "Cat1", jurisdiction=other_jurisdiction)
        checklist, errors = resolve_checklist("Cat1", self.jurisdiction.id)
        self.assertIsNone(checklist)
        self.assertEqual(errors[0]["code"], "CHECKLIST_NOT_FOUND")

    def test_annual_inspection_maps_to_cat1_checklist(self):
        """
        End-to-end: 'Annual Inspection - tag' → canonical 'Cat1' →
        resolve_checklist finds checklist by ChecklistType.name='CAT1'.
        """
        self._make_checklist_with_type("CAT1", "Mass Annual Inspection")
        # normalize_row maps "Annual Inspection - ..." to "Cat1"
        row = {
            "date": "1/5/2026", "time": "8:00",
            "inspector": "Ronald Travers", "tag #": "282-P-12",
            "test type": "Annual Inspection - 282-P-12",
        }
        result = normalize_row(row, 1)
        self.assertEqual(result["resolved"]["checklist_name"], "Cat1")

        checklist, errors = resolve_checklist("Cat1", self.jurisdiction.id)
        self.assertIsNotNone(checklist)
        self.assertEqual(errors, [])

    def test_annual_weight_test_maps_to_cat5_checklist(self):
        self._make_checklist_with_type("CAT5", "Mass Weight Test")
        row = {
            "date": "1/5/2026", "time": "8:00",
            "inspector": "Ronald Travers", "tag #": "282-P-12",
            "test type": "Annual Weight Test - 282-P-12",
        }
        result = normalize_row(row, 1)
        self.assertEqual(result["resolved"]["checklist_name"], "Cat5")

        checklist, errors = resolve_checklist("Cat5", self.jurisdiction.id)
        self.assertIsNotNone(checklist)
        self.assertEqual(errors, [])


# ---------------------------------------------------------------------------
# 15. 90-day annual rows are skipped before resolution (no DB side effects)
# ---------------------------------------------------------------------------

class NinetyDayAnnualSkippedBeforeResolutionTest(TestCase):
    """
    Rows with unsupported test type ('90 Day Annual') must be marked 'skipped'
    by normalize_row() and must NOT reach device/checklist resolution.
    The view-level guard (status in ('error', 'skipped')) prevents spurious
    DEVICE_NOT_FOUND / CHECKLIST_NOT_FOUND errors on these rows.
    """

    def test_normalize_row_marks_90_day_as_skipped(self):
        row = {
            "date": "1/5/2026", "time": "8:00",
            "inspector": "Ronald Travers", "tag #": "282-P-12",
            "test type": "90 Day Annual - 282-P-12",
        }
        result = normalize_row(row, 1)
        self.assertEqual(result["status"], "skipped")
        warning_codes = [w["code"] for w in result["warnings"]]
        self.assertIn("UNSUPPORTED_TEST_TYPE", warning_codes)
        error_codes = [e["code"] for e in result["errors"]]
        self.assertNotIn("UNKNOWN_TEST_TYPE", error_codes)
        self.assertNotIn("DEVICE_NOT_FOUND", error_codes)
        self.assertNotIn("CHECKLIST_NOT_FOUND", error_codes)


# ---------------------------------------------------------------------------
# 16. strip_test_type_tag_suffix — unit tests
# ---------------------------------------------------------------------------

class StripTestTypeSuffixTest(TestCase):
    """
    Unit tests for strip_test_type_tag_suffix().

    The function must strip the device-tag suffix embedded in Test Type values
    ("<type text> - <tag>") so only the semantic type text is passed to the
    inspection-type mapper.
    """

    def test_annual_inspection_with_tag(self):
        self.assertEqual(
            strip_test_type_tag_suffix("Annual Inspection - 282-P-12", "282-P-12"),
            "Annual Inspection",
        )

    def test_annual_weight_test_with_tag(self):
        self.assertEqual(
            strip_test_type_tag_suffix("Annual Weight Test - 282-P-12", "282-P-12"),
            "Annual Weight Test",
        )

    def test_90_day_annual_with_tag(self):
        self.assertEqual(
            strip_test_type_tag_suffix("90 Day Annual - 282-P-12", "282-P-12"),
            "90 Day Annual",
        )

    def test_no_tag_suffix_returns_original(self):
        """Plain values like 'Cat1' or 'Periodic' pass through unchanged."""
        self.assertEqual(strip_test_type_tag_suffix("Cat1", "282-P-12"), "Cat1")
        self.assertEqual(strip_test_type_tag_suffix("Periodic", "ELV-999"), "Periodic")

    def test_empty_tag_returns_original(self):
        self.assertEqual(
            strip_test_type_tag_suffix("Annual Inspection - 282-P-12", ""),
            "Annual Inspection - 282-P-12",
        )

    def test_case_insensitive_suffix_match(self):
        """Tag casing difference between columns must be tolerated."""
        self.assertEqual(
            strip_test_type_tag_suffix("Annual Inspection - 282-p-12", "282-P-12"),
            "Annual Inspection",
        )

    def test_extra_whitespace_is_trimmed(self):
        self.assertEqual(
            strip_test_type_tag_suffix("  Annual Inspection - 282-P-12  ", "282-P-12"),
            "Annual Inspection",
        )

    def test_partial_match_not_stripped(self):
        """Only a true suffix match strips; a mid-string occurrence is left alone."""
        self.assertEqual(
            strip_test_type_tag_suffix("282-P-12 Annual Inspection - OTHER", "282-P-12"),
            "282-P-12 Annual Inspection - OTHER",
        )


# ---------------------------------------------------------------------------
# 17. normalize_row — full tag-suffix stripping end-to-end
# ---------------------------------------------------------------------------

class NormalizeRowTagSuffixStrippingTest(TestCase):
    """
    Verify that normalize_row() strips the device-tag suffix from the raw
    Test Type field before performing inspection-type mapping.
    """

    def _row(self, test_type, tag="282-P-12"):
        return {
            "date": "1/5/2026",
            "time": "8:00",
            "inspector": "Ronald Travers",
            "tag #": tag,
            "test type": test_type,
        }

    def test_annual_inspection_suffix_resolves_to_cat1(self):
        result = normalize_row(self._row("Annual Inspection - 282-P-12"), row_index=1)
        self.assertEqual(result["resolved"]["checklist_name"], "Cat1")
        self.assertEqual(result["status"], "valid")

    def test_annual_weight_test_suffix_resolves_to_cat5(self):
        result = normalize_row(self._row("Annual Weight Test - 282-P-12"), row_index=1)
        self.assertEqual(result["resolved"]["checklist_name"], "Cat5")
        self.assertEqual(result["status"], "valid")

    def test_90_day_annual_suffix_is_skipped(self):
        result = normalize_row(self._row("90 Day Annual - 282-P-12"), row_index=1)
        self.assertEqual(result["status"], "skipped")
        self.assertIn("UNSUPPORTED_TEST_TYPE", [w["code"] for w in result["warnings"]])

    def test_plain_cat1_without_suffix_still_works(self):
        """Backward compat: existing short-form values must still resolve."""
        result = normalize_row(self._row("Cat1"), row_index=1)
        self.assertEqual(result["resolved"]["checklist_name"], "Cat1")
        self.assertEqual(result["status"], "valid")

    def test_plain_5_year_without_suffix_still_works(self):
        result = normalize_row(self._row("5 year"), row_index=1)
        self.assertEqual(result["resolved"]["checklist_name"], "Cat5")
        self.assertEqual(result["status"], "valid")

    def test_unknown_cleaned_type_returns_error(self):
        """After stripping the suffix, if the type is still unknown → UNKNOWN_TEST_TYPE."""
        result = normalize_row(self._row("Biennial Safety Check - 282-P-12"), row_index=1)
        self.assertEqual(result["status"], "error")
        self.assertIn("UNKNOWN_TEST_TYPE", [e["code"] for e in result["errors"]])

    def test_different_tag_number_not_stripped(self):
        """Suffix only stripped when it matches the Tag # column value."""
        # Tag # is "ELV-999", test type ends with "282-P-12" — no match, no strip
        result = normalize_row(self._row("Annual Inspection - 282-P-12", tag="ELV-999"), row_index=1)
        # "Annual Inspection - 282-P-12" does NOT end with "ELV-999", so it
        # passes through as-is, which still maps to Cat1 via substring match
        self.assertEqual(result["resolved"]["checklist_name"], "Cat1")