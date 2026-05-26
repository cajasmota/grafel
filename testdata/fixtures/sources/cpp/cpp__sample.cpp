// cpp__sample.cpp — fixture C/C++ tree-sitter extractor tests.
#include <iostream>
#include <vector>
#include "mylib.h"

#define MAX_SIZE 1024
#define PI 3.14159

namespace geometry {

enum class Color {
    Red,
    Green,
    Blue
};

struct Point {
    double x;
    double y;
};

class Shape {
public:
    Shape() {}
    virtual ~Shape() {}
    virtual double area() const = 0;
    virtual std::string name() const = 0;
};

class Circle : public Shape {
public:
    explicit Circle(double radius) : radius_(radius) {}

    double area() const override {
        return PI * radius_ * radius_;
    }

    std::string name() const override {
        return "Circle";
    }

private:
    double radius_;
};

class Rectangle : public Shape {
public:
    Rectangle(double w, double h) : width_(w), height_(h) {}

    double area() const override {
        return width_ * height_;
    }

    std::string name() const override {
        return "Rectangle";
    }

private:
    double width_;
    double height_;
};

template <typename T>
class Container {
public:
    void add(T item) {
        items_.push_back(item);
    }

    T get(int index) const {
        return items_[index];
    }

    int size() const {
        return static_cast<int>(items_.size());
    }

private:
    std::vector<T> items_;
};

template <typename T>
T clamp(T value, T low, T high) {
    if (value < low) return low;
    if (value > high) return high;
    return value;
}

double computeArea(const Shape& s) {
    return s.area();
}

void printShapes(const std::vector<Shape*>& shapes) {
    for (const auto& s : shapes) {
        std::cout << s->name() << ": " << s->area() << std::endl;
    }
}

} // namespace geometry

namespace utils {

struct Config {
    int maxRetries;
    double timeout;
    bool verbose;
};

template <typename Key, typename Value>
struct Pair {
    Key first;
    Value second;
};

void logMessage(const std::string& msg) {
    std::cout << "[LOG] " << msg << std::endl;
}

int clampInt(int v, int lo, int hi) {
    if (v < lo) return lo;
    if (v > hi) return hi;
    return v;
}

} // namespace utils

int main() {
    geometry::Circle c(5.0);
    geometry::Rectangle r(3.0, 4.0);
    std::cout << c.area() << std::endl;
    std::cout << r.area() << std::endl;
    return 0;
}
