# Source: https://github.com/sqlalchemy/sqlalchemy/blob/main/lib/sqlalchemy/orm/properties.py | License: MIT
# orm/properties.py
# Copyright (C) 2005-2026 the SQLAlchemy authors and contributors
# <see AUTHORS file>
#
# This module is part of SQLAlchemy and is released under
# the MIT License: https://www.opensource.org/licenses/mit-license.php

"""MapperProperty implementations.

This is a private module which defines the behavior of individual ORM-
mapped attributes.

"""

from __future__ import annotations

from typing import Any
from typing import cast
from typing import Dict
from typing import get_args
from typing import List
from typing import Optional
from typing import Sequence
from typing import Set
from typing import Tuple
from typing import Type
from typing import TYPE_CHECKING
from typing import TypeVar
from typing import Union

from . import attributes
from . import exc as orm_exc
from . import strategy_options
from .base import _DeclarativeMapped
from .base import class_mapper
from .descriptor_props import CompositeProperty
from .descriptor_props import ConcreteInheritedProperty
from .descriptor_props import SynonymProperty
from .interfaces import _AttributeOptions
from .interfaces import _DataclassDefaultsDontSet
from .interfaces import _DEFAULT_ATTRIBUTE_OPTIONS
from .interfaces import _IntrospectsAnnotations
from .interfaces import _MapsColumns
from .interfaces import MapperProperty
from .interfaces import PropComparator
from .interfaces import StrategizedProperty
from .relationships import RelationshipProperty
from .util import de_stringify_annotation
from .. import exc as sa_exc
from .. import ForeignKey
from .. import log
from .. import util
from ..sql import coercions
from ..sql import roles
from ..sql.base import _NoArg
from ..sql.schema import Column
from ..sql.schema import SchemaConst
from ..sql.type_api import TypeEngine
from ..util.typing import de_optionalize_union_types
from ..util.typing import includes_none
from ..util.typing import is_a_type
from ..util.typing import is_fwd_ref
from ..util.typing import is_pep593
from ..util.typing import is_pep695
from ..util.typing import Self

if TYPE_CHECKING:
    from typing import ForwardRef

    from ._typing import _IdentityKeyType
    from ._typing import _InstanceDict
    from ._typing import _ORMColumnExprArgument
    from ._typing import _RegistryType
    from .base import Mapped
    from .decl_base import _DeclarativeMapperConfig
    from .mapper import Mapper
    from .session import Session
    from .state import _InstallLoaderCallableProto
    from .state import InstanceState
    from ..sql._typing import _InfoType
    from ..sql.elements import ColumnElement
    from ..sql.elements import NamedColumn
    from ..sql.operators import OperatorType
    from ..util.typing import _AnnotationScanType
    from ..util.typing import _MatchedOnType
    from ..util.typing import RODescriptorReference

_T = TypeVar("_T", bound=Any)
_PT = TypeVar("_PT", bound=Any)
_NC = TypeVar("_NC", bound="NamedColumn[Any]")

__all__ = [
    "ColumnProperty",
    "CompositeProperty",
    "ConcreteInheritedProperty",
    "RelationshipProperty",
    "SynonymProperty",
]


@log.class_logger
class ColumnProperty(
    _DataclassDefaultsDontSet,
    _MapsColumns[_T],
    StrategizedProperty[_T],
    _IntrospectsAnnotations,
    log.Identified,
):
    """Describes an object attribute that corresponds to a table column
    or other column expression.

    Public constructor is the :func:`_orm.column_property` function.

    """

    strategy_wildcard_key = strategy_options._COLUMN_TOKEN
    inherit_cache = True
    """:meta private:"""

    _links_to_entity = False

    columns: List[NamedColumn[Any]]

    _is_polymorphic_discriminator: bool

    _mapped_by_synonym: Optional[str]

    comparator_factory: Type[PropComparator[_T]]

    __slots__ = (
        "columns",
        "group",
        "deferred",
        "instrument",
        "comparator_factory",
        "active_history",
        "expire_on_flush",
        "_default_scalar_value",
        "_creation_order",
        "_is_polymorphic_discriminator",
        "_mapped_by_synonym",
        "_deferred_column_loader",
        "_raise_column_loader",
        "_renders_in_subqueries",
        "raiseload",
    )

    def __init__(
        self,
        column: _ORMColumnExprArgument[_T],
        *additional_columns: _ORMColumnExprArgument[Any],
        attribute_options: Optional[_AttributeOptions] = None,
        group: Optional[str] = None,
        deferred: bool = False,
        raiseload: bool = False,
        comparator_factory: Optional[Type[PropComparator[_T]]] = None,
        active_history: bool = False,
        default_scalar_value: Any = None,
        expire_on_flush: bool = True,
        info: Optional[_InfoType] = None,
        doc: Optional[str] = None,
        _instrument: bool = True,
        _assume_readonly_dc_attributes: bool = False,
    ):
        super().__init__(
            attribute_options=attribute_options,
            _assume_readonly_dc_attributes=_assume_readonly_dc_attributes,
        )
        columns = (column,) + additional_columns
        self.columns = [
            coercions.expect(roles.LabeledColumnExprRole, c) for c in columns
        ]
        self.group = group
        self.deferred = deferred
        self.raiseload = raiseload
        self.instrument = _instrument
        self.comparator_factory = (
            comparator_factory
            if comparator_factory is not None
            else self.__class__.Comparator
        )
        self.active_history = active_history
        self._default_scalar_value = default_scalar_value
        self.expire_on_flush = expire_on_flush

        if info is not None:
            self.info.update(info)

        if doc is not None:
            self.doc = doc
        else:
            for col in reversed(self.columns):
                doc = getattr(col, "doc", None)
                if doc is not None:
                    self.doc = doc
                    break
            else:
                self.doc = None

        util.set_creation_order(self)

        self.strategy_key = (
            ("deferred", self.deferred),
            ("instrument", self.instrument),
        )
        if self.raiseload:
            self.strategy_key += (("raiseload", True),)

    def declarative_scan(
        self,
        decl_scan: _DeclarativeMapperConfig,
        registry: _RegistryType,
        cls: Type[Any],
        originating_module: Optional[str],
        key: str,
        mapped_container: Optional[Type[Mapped[Any]]],
        annotation: Optional[_AnnotationScanType],
        extracted_mapped_annotation: Optional[_AnnotationScanType],
        is_dataclass_field: bool,
    ) -> None:
        column = self.columns[0]
        if column.key is None:
            column.key = key
        if column.name is None:
            column.name = key

    @property
    def mapper_property_to_assign(self) -> Optional[MapperProperty[_T]]:
        return self

    @property
    def columns_to_assign(self) -> List[Tuple[Column[Any], int]]:
        # mypy doesn't care about the isinstance here
        return [
            (c, 0)  # type: ignore
            for c in self.columns
            if isinstance(c, Column) and c.table is None
        ]

    def _memoized_attr__renders_in_subqueries(self) -> bool:
        if ("query_expression", True) in self.strategy_key:
            return self.strategy._have_default_expression  # type: ignore

        return ("deferred", True) not in self.strategy_key or (
            self not in self.parent._readonly_props
        )

    @util.preload_module("sqlalchemy.orm.state", "sqlalchemy.orm.strategies")
    def _memoized_attr__deferred_column_loader(
        self,
    ) -> _InstallLoaderCallableProto[Any]:
        state = util.preloaded.orm_state
        strategies = util.preloaded.orm_strategies
        return state.InstanceState._instance_level_callable_processor(
            self.parent.class_manager,
            strategies._LoadDeferredColumns(self.key),
            self.key,
        )

    @util.preload_module("sqlalchemy.orm.state", "sqlalchemy.orm.strategies")
    def _memoized_attr__raise_column_loader(
        self,
    ) -> _InstallLoaderCallableProto[Any]:
        state = util.preloaded.orm_state
        strategies = util.preloaded.orm_strategies
        return state.InstanceState._instance_level_callable_processor(
            self.parent.class_manager,
            strategies._LoadDeferredColumns(self.key, True),
            self.key,
        )

    def __clause_element__(self) -> roles.ColumnsClauseRole:
        """Allow the ColumnProperty to work in expression before it is turned
        into an instrumented attribute.
        """

        return self.expression

    @property
    def expression(self) -> roles.ColumnsClauseRole:
        """Return the primary column or expression for this ColumnProperty.

        E.g.::


            class File(Base):
                # ...

                name = Column(String(64))
                extension = Column(String(8))
                filename = column_property(name + "." + extension)
                path = column_property("C:/" + filename.expression)

        .. seealso::

            :ref:`mapper_column_property_sql_expressions_composed`

        """
        return self.columns[0]

    def instrument_class(self, mapper: Mapper[Any]) -> None:
        if not self.instrument:
            return

        attributes._register_descriptor(
            mapper.class_,
            self.key,
            comparator=self.comparator_factory(self, mapper),
            parententity=mapper,
            doc=self.doc,
        )

    def do_init(self) -> None:
        super().do_init()

        if len(self.columns) > 1 and set(self.parent.primary_key).issuperset(
            self.columns
        ):
            util.warn(
                (
                    "On mapper %s, primary key column '%s' is being combined "
                    "with distinct primary key column '%s' in attribute '%s'. "
                    "Use explicit properties to give each column its own "
                    "mapped attribute name."
                )
                % (self.parent, self.columns[1], self.columns[0], self.key)
            )

    def copy(self) -> ColumnProperty[_T]:
        return ColumnProperty(
            *self.columns,
            deferred=self.deferred,
            group=self.group,
            active_history=self.active_history,
            default_scalar_value=self._default_scalar_value,
        )

    def merge(
        self,
        session: Session,
        source_state: InstanceState[Any],
        source_dict: _InstanceDict,
        dest_state: InstanceState[Any],
        dest_dict: _InstanceDict,
        load: bool,
        _recursive: Dict[Any, object],
        _resolve_conflict_map: Dict[_IdentityKeyType[Any], object],
    ) -> None:
        if not self.instrument:
            return
        elif self.key in source_dict:
            value = source_dict[self.key]

            if not load:
                dest_dict[self.key] = value
            else:
                impl = dest_state.get_impl(self.key)
                impl.set(dest_state, dest_dict, value, None)
        elif dest_state.has_identity and self.key not in dest_dict:
            dest_state._expire_attributes(
                dest_dict, [self.key], no_loader=True
            )

    class Comparator(util.MemoizedSlots, PropComparator[_PT]):
        """Produce boolean, comparison, and other operators for
        :class:`.ColumnProperty` attributes.

        See the documentation for :class:`.PropComparator` for a brief
        overview.

        .. seealso::

            :class:`.PropComparator`

            :class:`.ColumnOperators`

            :ref:`types_operators`

            :attr:`.TypeEngine.comparator_factory`

        """

        if not TYPE_CHECKING:
            # prevent pylance from being clever about slots
            __slots__ = "__clause_element__", "info", "expressions"

        prop: RODescriptorReference[ColumnProperty[_PT]]

        expressions: Sequence[NamedColumn[Any]]
        """The full sequence of columns referenced by this
         attribute, adjusted for any aliasing in progress.

        .. seealso::

           :ref:`maptojoin` - usage example
        """

        def _orm_annotate_column(self, column: _NC) -> _NC:
            """annotate and possibly adapt a column to be returned
            as the mapped-attribute exposed version of the column.

            The column in this context needs to act as much like the
            column in an ORM mapped context as possible, so includes
            annotations to give hints to various ORM functions as to
            the source entity of this column.   It also adapts it
            to the mapper's with_polymorphic selectable if one is
            present.

            """

            pe = self._parententity
            annotations: Dict[str, Any] = {
                "entity_namespace": pe,
                "parententity": pe,
                "parentmapper": pe,
                "proxy_key": self.prop.key,
            }

            col = column

            # for a mapper with polymorphic_on and an adapter, return
            # the column against the polymorphic selectable.
            # see also orm.util._orm_downgrade_polymorphic_columns
            # for the reverse operation.
            if self._parentmapper._polymorphic_adapter:
                mapper_local_col = col
                col = self._parentmapper._polymorphic_adapter.traverse(col)

                # this is a clue to the ORM Query etc. that this column
                # was adapted to the mapper's polymorphic_adapter.  the
                # ORM uses this hint to know which column its adapting.
                annotations["adapt_column"] = mapper_local_col

            return col._annotate(annotations)._set_propagate_attrs(
                {"compile_state_plugin": "orm", "plugin_subject": pe}
            )

        if TYPE_CHECKING:

            def __clause_element__(self) -> NamedColumn[_PT]: ...

        def _memoized_method___clause_element__(
            self,
        ) -> NamedColumn[_PT]:
            if self.adapter:
                return self.adapter(self.prop.columns[0], self.prop.key)
            else:
                return self._orm_annotate_column(self.prop.columns[0])

        def _memoized_attr_info(self) -> _InfoType:
            """The .info dictionary for this attribute."""

            ce = self.__clause_element__()
            try:
                return ce.info  # type: ignore
            except AttributeError:
                return self.prop.info

        def _memoized_attr_expressions(self) -> Sequence[NamedColumn[Any]]:
            """The full sequence of columns referenced by this
            attribute, adjusted for any aliasing in progress.

            """
            if self.adapter:
                return [
                    self.adapter(col, self.prop.key)
                    for col in self.prop.columns
                ]
            else:
                return [
                    self._orm_annotate_column(col) for col in self.prop.columns
                ]

        def _fallback_getattr(self, key: str) -> Any:
            """proxy attribute access down to the mapped column.

            this allows user-defined comparison methods to be accessed.
            """
            return getattr(self.__clause_element__(), key)

        def operate(
            self, op: OperatorType, *other: Any, **kwargs: Any
        ) -> ColumnElement[Any]:
            return op(self.__clause_element__(), *other, **kwargs)  # type: ignore[no-any-return]  # noqa: E501

        def reverse_operate(
            self, op: OperatorType, other: Any, **kwargs: Any
        ) -> ColumnElement[Any]:
            col = self.__clause_element__()
            return op(col._bind_param(op, other), col, **kwargs)  # type: ignore[no-any-return]  # noqa: E501

    def __str__(self) -> str:
        if not self.parent or not self.key:
            return object.__repr__(self)
        return str(self.parent.class_.__name__) + "." + self.key


class MappedSQLExpression(ColumnProperty[_T], _DeclarativeMapped[_T]):
    """Declarative front-end for the :class:`.ColumnProperty` class.

