package com.example.kafka;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.kafka.annotation.KafkaListener;
import org.springframework.kafka.core.KafkaTemplate;
import org.springframework.web.bind.annotation.*;
import org.springframework.http.ResponseEntity;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.data.jpa.repository.JpaRepository;
import javax.persistence.*;
import java.util.List;

/**
 * Java Spring Boot REST + Kafka producer/consumer fixture.
 * Demonstrates: HTTP endpoint, Kafka producer, Kafka consumer, DB access.
 */
@SpringBootApplication
public class SpringKafkaApp {
    public static void main(String[] args) {
        SpringApplication.run(SpringKafkaApp.class, args);
    }
}

@RestController
@RequestMapping("/api/orders")
class OrderController {

    @Autowired
    private KafkaTemplate<String, String> kafkaTemplate;

    @Autowired
    private OrderRepository orderRepository;

    @GetMapping
    public ResponseEntity<List<Order>> listOrders() {
        return ResponseEntity.ok(orderRepository.findAll());
    }

    @PostMapping
    public ResponseEntity<Order> createOrder(@RequestBody Order order) {
        Order saved = orderRepository.save(order);
        kafkaTemplate.send("orders.created", String.valueOf(saved.getId()), saved.toString());
        return ResponseEntity.status(201).body(saved);
    }

    @GetMapping("/{id}")
    public ResponseEntity<Order> getOrder(@PathVariable Long id) {
        return orderRepository.findById(id)
            .map(ResponseEntity::ok)
            .orElse(ResponseEntity.notFound().build());
    }
}

@org.springframework.stereotype.Service
class OrderEventConsumer {

    @Autowired
    private OrderRepository orderRepository;

    @KafkaListener(topics = "orders.created", groupId = "order-processor")
    public void handleOrderCreated(String payload) {
        // process the new order event
        System.out.println("Processing order event: " + payload);
    }

    @KafkaListener(topics = "orders.cancelled", groupId = "order-processor")
    public void handleOrderCancelled(String orderId) {
        Long id = Long.parseLong(orderId.trim());
        orderRepository.findById(id).ifPresent(o -> {
            o.setStatus("CANCELLED");
            orderRepository.save(o);
        });
    }
}

@Entity
@Table(name = "orders")
class Order {
    @Id
    @GeneratedValue(strategy = GenerationType.IDENTITY)
    private Long id;

    @Column(nullable = false)
    private String product;

    @Column(nullable = false)
    private int quantity;

    @Column(nullable = false)
    private String status = "PENDING";

    public Long getId() { return id; }
    public void setId(Long id) { this.id = id; }
    public String getProduct() { return product; }
    public void setProduct(String product) { this.product = product; }
    public int getQuantity() { return quantity; }
    public void setQuantity(int quantity) { this.quantity = quantity; }
    public String getStatus() { return status; }
    public void setStatus(String status) { this.status = status; }

    @Override
    public String toString() {
        return "{\"id\":" + id + ",\"product\":\"" + product + "\",\"quantity\":" + quantity + "}";
    }
}

interface OrderRepository extends JpaRepository<Order, Long> {
}
