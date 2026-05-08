// Source: https://github.com/spring-projects/spring-data-jpa/blob/main/spring-data-jpa/src/main/java/org/springframework/data/jpa/repository/support/SimpleJpaRepository.java | License: Apache-2.0
/*
 * Copyright 2008-present the original author or authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package org.springframework.data.jpa.repository.support;

import static org.springframework.data.jpa.repository.query.QueryUtils.*;

import jakarta.persistence.EntityManager;
import jakarta.persistence.LockModeType;
import jakarta.persistence.Query;
import jakarta.persistence.TypedQuery;
import jakarta.persistence.criteria.CriteriaBuilder;
import jakarta.persistence.criteria.CriteriaDelete;
import jakarta.persistence.criteria.CriteriaQuery;
import jakarta.persistence.criteria.CriteriaUpdate;
import jakarta.persistence.criteria.Path;
import jakarta.persistence.criteria.Predicate;
import jakarta.persistence.criteria.Root;
import jakarta.persistence.criteria.Selection;

import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.function.BiConsumer;
import java.util.function.Function;

import org.jspecify.annotations.Nullable;

import org.springframework.dao.InvalidDataAccessApiUsageException;
import org.springframework.data.core.PropertyPath;
import org.springframework.data.domain.Example;
import org.springframework.data.domain.KeysetScrollPosition;
import org.springframework.data.domain.OffsetScrollPosition;
import org.springframework.data.domain.Page;
import org.springframework.data.domain.PageImpl;
import org.springframework.data.domain.Pageable;
import org.springframework.data.domain.ScrollPosition;
import org.springframework.data.domain.Sort;
import org.springframework.data.jpa.convert.QueryByExamplePredicateBuilder;
import org.springframework.data.jpa.domain.DeleteSpecification;
import org.springframework.data.jpa.domain.Specification;
import org.springframework.data.jpa.domain.UpdateSpecification;
import org.springframework.data.jpa.provider.PersistenceProvider;
import org.springframework.data.jpa.repository.EntityGraph;
import org.springframework.data.jpa.repository.query.EscapeCharacter;
import org.springframework.data.jpa.repository.query.KeysetScrollDelegate;
import org.springframework.data.jpa.repository.query.KeysetScrollSpecification;
import org.springframework.data.jpa.repository.query.QueryUtils;
import org.springframework.data.jpa.repository.support.FetchableFluentQueryBySpecification.SpecificationScrollDelegate;
import org.springframework.data.jpa.repository.support.FluentQuerySupport.ScrollQueryFactory;
import org.springframework.data.jpa.repository.support.QueryHints.NoHints;
import org.springframework.data.jpa.support.PageableUtils;
import org.springframework.data.projection.ProjectionFactory;
import org.springframework.data.projection.SpelAwareProxyProjectionFactory;
import org.springframework.data.repository.query.FluentQuery;
import org.springframework.data.repository.query.FluentQuery.FetchableFluentQuery;
import org.springframework.data.repository.query.ReturnedType;
import org.springframework.data.support.PageableExecutionUtils;
import org.springframework.data.util.Lazy;
import org.springframework.data.util.ProxyUtils;
import org.springframework.data.util.Streamable;
import org.springframework.stereotype.Repository;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.util.Assert;

/**
 * Default implementation of the {@link org.springframework.data.repository.CrudRepository} interface. This will offer
 * you a more sophisticated interface than the plain {@link EntityManager} .
 *
 * @param <T> the type of the entity to handle
 * @param <ID> the type of the entity's identifier
 * @author Oliver Gierke
 * @author Eberhard Wolff
 * @author Thomas Darimont
 * @author Mark Paluch
 * @author Christoph Strobl
 * @author Stefan Fussenegger
 * @author Jens Schauder
 * @author David Madden
 * @author Moritz Becker
 * @author Sander Krabbenborg
 * @author Jesse Wouters
 * @author Greg Turnquist
 * @author Yanming Zhou
 * @author Ernst-Jan van der Laan
 * @author Diego Krupitza
 * @author Seol-JY
 * @author Joshua Chen
 * @author Giheon Do
 */
@Repository
@Transactional(readOnly = true)
public class SimpleJpaRepository<T, ID> implements JpaRepositoryImplementation<T, ID> {

	private static final String ID_MUST_NOT_BE_NULL = "The given id must not be null";
	private static final String IDS_MUST_NOT_BE_NULL = "Ids must not be null";
	private static final String ENTITY_MUST_NOT_BE_NULL = "Entity must not be null";
	private static final String ENTITIES_MUST_NOT_BE_NULL = "Entities must not be null";
	private static final String EXAMPLE_MUST_NOT_BE_NULL = "Example must not be null";
	private static final String SPECIFICATION_MUST_NOT_BE_NULL = "Specification must not be null";
	private static final String QUERY_FUNCTION_MUST_NOT_BE_NULL = "Query function must not be null";

	private final JpaEntityInformation<T, ?> entityInformation;
	private final EntityManager entityManager;
	private final PersistenceProvider provider;

	private final Lazy<String> deleteAllQueryString;
	private final Lazy<String> countQueryString;

	private @Nullable CrudMethodMetadata metadata;
	private ProjectionFactory projectionFactory;
	private EscapeCharacter escapeCharacter = EscapeCharacter.DEFAULT;

	/**
	 * Creates a new {@link SimpleJpaRepository} to manage objects of the given {@link JpaEntityInformation}.
	 *
	 * @param entityInformation must not be {@literal null}.
	 * @param entityManager must not be {@literal null}.
	 */
	public SimpleJpaRepository(JpaEntityInformation<T, ?> entityInformation, EntityManager entityManager) {

		Assert.notNull(entityInformation, "JpaEntityInformation must not be null");
		Assert.notNull(entityManager, "EntityManager must not be null");

		this.entityInformation = entityInformation;
		this.entityManager = entityManager;
		this.provider = PersistenceProvider.fromEntityManager(entityManager);
		this.projectionFactory = new SpelAwareProxyProjectionFactory();

		this.deleteAllQueryString = Lazy
				.of(() -> getQueryString(DELETE_ALL_QUERY_STRING, entityInformation.getEntityName()));
		this.countQueryString = Lazy
				.of(() -> getQueryString(String.format(COUNT_QUERY_STRING, provider.getCountQueryPlaceholder(), "%s"),
						entityInformation.getEntityName()));
	}

	/**
	 * Creates a new {@link SimpleJpaRepository} to manage objects of the given domain type.
	 *
	 * @param domainClass must not be {@literal null}.
	 * @param entityManager must not be {@literal null}.
	 */
	public SimpleJpaRepository(Class<T> domainClass, EntityManager entityManager) {
		this(JpaEntityInformationSupport.getEntityInformation(domainClass, entityManager), entityManager);
	}

	/**
	 * Configures a custom {@link CrudMethodMetadata} to be used to detect {@link LockModeType}s and query hints to be
	 * applied to queries.
	 *
	 * @param metadata custom {@link CrudMethodMetadata} to be used, can be {@literal null}.
	 */
	@Override
	public void setRepositoryMethodMetadata(CrudMethodMetadata metadata) {
		this.metadata = metadata;
	}

	@Override
	public void setEscapeCharacter(EscapeCharacter escapeCharacter) {
		this.escapeCharacter = escapeCharacter;
	}

	@Override
	public void setProjectionFactory(ProjectionFactory projectionFactory) {
		this.projectionFactory = projectionFactory;
	}

	protected @Nullable CrudMethodMetadata getRepositoryMethodMetadata() {
		return metadata;
	}

	protected Class<T> getDomainClass() {
		return entityInformation.getJavaType();
	}

	@Override
	@Transactional
	public void deleteById(ID id) {

		Assert.notNull(id, ID_MUST_NOT_BE_NULL);

		findById(id).ifPresent(this::delete);
	}

	@Override
	@Transactional
	public void delete(T entity) {

		Assert.notNull(entity, ENTITY_MUST_NOT_BE_NULL);

		doDelete(entityManager, entityInformation, entity);
	}

	static <T> boolean doDelete(EntityManager entityManager, JpaEntityInformation<T, ?> entityInformation, T entity) {

		if (entityInformation.isNew(entity)) {
			return false;
		}

		if (entityManager.contains(entity)) {
			entityManager.remove(entity);
			return true;
		}

		Class<?> type = ProxyUtils.getUserClass(entity);

		// if the entity to be deleted doesn't exist, delete is a NOOP
		T existing = (T) entityManager.find(type, entityInformation.getId(entity));
		if (existing != null) {
			entityManager.remove(entityManager.merge(entity));

			return true;
		}

		return false;
	}

	@Override
	@Transactional
	public void deleteAllById(Iterable<? extends ID> ids) {

		Assert.notNull(ids, IDS_MUST_NOT_BE_NULL);

		for (ID id : ids) {
			deleteById(id);
		}
	}

	@Override
	@Transactional
	public void deleteAllByIdInBatch(Iterable<ID> ids) {

		Assert.notNull(ids, IDS_MUST_NOT_BE_NULL);

		if (!ids.iterator().hasNext()) {
			return;
		}

		if (entityInformation.hasCompositeId()) {

			List<T> entities = new ArrayList<>();
			// generate entity (proxies) without accessing the database.
			ids.forEach(id -> entities.add(getReferenceById(id)));
			deleteAllInBatch(entities);
		} else {

			String queryString = String.format(DELETE_ALL_QUERY_BY_ID_STRING, entityInformation.getEntityName(),
					entityInformation.getRequiredIdAttribute().getName());

			Query query = entityManager.createQuery(queryString);

			/*
			 * Some JPA providers require {@code ids} to be a {@link Collection} so we must convert if it's not already.
			 */
			Collection<ID> idCollection = toCollection(ids);
			query.setParameter("ids", idCollection);

			applyQueryHints(query);

			query.executeUpdate();
		}
	}

	@Override
	@Transactional
	public void deleteAll(Iterable<? extends T> entities) {

		Assert.notNull(entities, ENTITIES_MUST_NOT_BE_NULL);

		for (T entity : entities) {
			delete(entity);
		}
	}

	@Override
	@Transactional
	public void deleteAllInBatch(Iterable<T> entities) {

		Assert.notNull(entities, ENTITIES_MUST_NOT_BE_NULL);

		if (!entities.iterator().hasNext()) {
			return;
		}

		applyAndBind(getQueryString(DELETE_ALL_QUERY_STRING, entityInformation.getEntityName()), entities, entityManager)
				.executeUpdate();
	}

	@Override
	@Transactional
	public void deleteAll() {

		for (T element : findAll()) {
			delete(element);
		}
	}

	@Override
	@Transactional
	public void deleteAllInBatch() {

		Query query = entityManager.createQuery(deleteAllQueryString.get());

		applyQueryHints(query);

		query.executeUpdate();
	}

	@Override
	public Optional<T> findById(ID id) {

		Assert.notNull(id, ID_MUST_NOT_BE_NULL);

		Class<T> domainType = getDomainClass();

		if (metadata == null) {
			return Optional.ofNullable(entityManager.find(domainType, id));
		}

		LockModeType type = metadata.getLockModeType();
		Map<String, Object> hints = getHints();

		return Optional.ofNullable(
				type == null ? entityManager.find(domainType, id, hints) : entityManager.find(domainType, id, type, hints));
	}

	@Deprecated
	@Override
	public T getOne(ID id) {
		return getReferenceById(id);
	}

	@Deprecated
	@Override
	public T getById(ID id) {
		return getReferenceById(id);
	}

	/*
	 * (non-Javadoc)
	 * @see org.springframework.data.jpa.repository.JpaRepository#getReferenceById(java.io.Serializable)
	 */
	@Override
	public T getReferenceById(ID id) {

		Assert.notNull(id, ID_MUST_NOT_BE_NULL);
		return entityManager.getReference(getDomainClass(), id);
	}

	@Override
	public boolean existsById(ID id) {

		Assert.notNull(id, ID_MUST_NOT_BE_NULL);

		if (entityInformation.getIdAttribute() == null) {
			return findById(id).isPresent();
		}

		String placeholder = provider.getCountQueryPlaceholder();
		String entityName = entityInformation.getEntityName();
		Iterable<String> idAttributeNames = entityInformation.getIdAttributeNames();
		String existsQuery = QueryUtils.getExistsQueryString(entityName, placeholder, idAttributeNames);

		TypedQuery<Long> query = entityManager.createQuery(existsQuery, Long.class);

		applyQueryHints(query);

		if (!entityInformation.hasCompositeId()) {
			query.setParameter(idAttributeNames.iterator().next(), id);
			return ExistsUtil.exists(query.getSingleResult());
		}

		for (String idAttributeName : idAttributeNames) {

			Object idAttributeValue = entityInformation.getCompositeIdAttributeValue(id, idAttributeName);

			boolean complexIdParameterValueDiscovered = idAttributeValue != null
					&& !query.getParameter(idAttributeName).getParameterType().isAssignableFrom(idAttributeValue.getClass());

			if (complexIdParameterValueDiscovered) {

				// fall-back to findById(id) which does the proper mapping for the parameter.
				return findById(id).isPresent();
			}

			query.setParameter(idAttributeName, idAttributeValue);
		}

		return ExistsUtil.exists(query.getSingleResult());
	}

	@Override
	public List<T> findAll() {
		return getQuery(Specification.unrestricted(), Sort.unsorted()).getResultList();
	}

	@Override
	public List<T> findAllById(Iterable<ID> ids) {

		Assert.notNull(ids, IDS_MUST_NOT_BE_NULL);

		if (!ids.iterator().hasNext()) {
			return Collections.emptyList();
		}

		if (entityInformation.hasCompositeId()) {

			List<T> results = new ArrayList<>();

			for (ID id : ids) {
				findById(id).ifPresent(results::add);
			}

			return results;
		}

		Collection<ID> idCollection = toCollection(ids);

		TypedQuery<T> query = getQuery((root, q, criteriaBuilder) -> {

			Path<?> path = root.get(entityInformation.getIdAttribute());
			return path.in(idCollection);

		}, Sort.unsorted());

		return query.getResultList();
	}

	@Override
	public List<T> findAll(Sort sort) {
		return getQuery(Specification.unrestricted(), sort).getResultList();
	}

	@Override
	public Page<T> findAll(Pageable pageable) {
		return findAll(Specification.unrestricted(), pageable);
	}

	@Override
	public Optional<T> findOne(Specification<T> spec) {
		return Optional.ofNullable(getQuery(spec, Sort.unsorted()).setMaxResults(2).getSingleResultOrNull());
	}

	@Override
	public List<T> findAll(Specification<T> spec) {
		return getQuery(spec, Sort.unsorted()).getResultList();
	}

	@Override
	public Page<T> findAll(Specification<T> spec, Pageable pageable) {
		return findAll(spec, spec, pageable);
	}

	@Override
	public Page<T> findAll(Specification<T> spec, Specification<T> countSpec, Pageable pageable) {

		Specification<T> specToUse = spec == null ? Specification.unrestricted() : spec;
		Specification<T> countSpecToUse = countSpec == null ? Specification.unrestricted() : countSpec;

		TypedQuery<T> query = getQuery(specToUse, pageable);
		return pageable.isUnpaged() ? new PageImpl<>(query.getResultList())
				: readPage(query, getDomainClass(), pageable, countSpecToUse);
	}

	@Override
	public List<T> findAll(Specification<T> spec, Sort sort) {
		return getQuery(spec, sort).getResultList();
	}

	@Override
	public boolean exists(Specification<T> spec) {

		Assert.notNull(spec, "Specification must not be null");

		CriteriaQuery<Integer> cq = this.entityManager.getCriteriaBuilder() //
				.createQuery(Integer.class) //
				.select(this.entityManager.getCriteriaBuilder().literal(1));

		applySpecificationToCriteria(spec, getDomainClass(), cq);

		TypedQuery<Integer> query = applyRepositoryMethodMetadata(this.entityManager.createQuery(cq));
		return ExistsUtil.exists(query.setMaxResults(1).getResultList().size());
