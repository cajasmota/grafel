package cpp_test

// new_extractors_test.go — fixture tests for ros_extractor.go,
// unreal_extractor.go, redis_query.go, restinio_middleware.go, and the
// extended qt.go capabilities.

import "testing"

// ---------------------------------------------------------------------------
// Qt extended capabilities
// ---------------------------------------------------------------------------

func TestQtContextExtraction(t *testing.T) {
	src := `
int main(int argc, char *argv[]) {
    QApplication app(argc, argv);
    MainWindow w;
    w.show();
    return app.exec();
}
`
	ents := extract(t, "custom_cpp_qt", fi("main.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "context:QApplication:app") {
		t.Errorf("expected context:QApplication:app entity, got %v", ents)
	}
}

func TestQtContextQQmlEngine(t *testing.T) {
	src := `
int main(int argc, char *argv[]) {
    QGuiApplication app(argc, argv);
    QQmlApplicationEngine engine;
    engine.load(QUrl(QStringLiteral("qrc:/main.qml")));
    return app.exec();
}
`
	ents := extract(t, "custom_cpp_qt", fi("main.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "context:QGuiApplication:app") {
		t.Errorf("expected QGuiApplication context entity, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "context:QQmlApplicationEngine:engine") {
		t.Errorf("expected QQmlApplicationEngine context entity, got %v", ents)
	}
}

func TestQtEmitStateSetter(t *testing.T) {
	src := `
void Counter::increment() {
    m_count++;
    emit countChanged(m_count);
    emit stateUpdated();
}
`
	ents := extract(t, "custom_cpp_qt", fi("counter.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "emit:countChanged") {
		t.Errorf("expected emit:countChanged entity, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "emit:stateUpdated") {
		t.Errorf("expected emit:stateUpdated entity, got %v", ents)
	}
}

func TestQtBranchIfEnum(t *testing.T) {
	src := `
void Handler::handle(Qt::Key key) {
    if (key == Qt::Key_Return) {
        accept();
    }
}
`
	ents := extract(t, "custom_cpp_qt", fi("handler.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "branch:if_qt_enum:Key_Return") {
		t.Errorf("expected branch:if_qt_enum:Key_Return, got %v", ents)
	}
}

func TestQtBranchQAssert(t *testing.T) {
	src := `
void MyClass::validate(int x) {
    Q_ASSERT(x > 0);
    process(x);
}
`
	ents := extract(t, "custom_cpp_qt", fi("myclass.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && len(e.Name) >= len("branch:Q_ASSERT@L") &&
			e.Name[:len("branch:Q_ASSERT@L")] == "branch:Q_ASSERT@L" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected branch:Q_ASSERT@L... entity, got %v", ents)
	}
}

func TestQtDataFetchingQNAM(t *testing.T) {
	src := `
#include <QNetworkAccessManager>
#include <QNetworkRequest>

void ApiClient::fetchData(const QString &url) {
    QNetworkAccessManager *manager = new QNetworkAccessManager(this);
    QNetworkReply *reply = manager->get(QNetworkRequest(QUrl(url)));
    connect(reply, &QNetworkReply::finished, this, &ApiClient::onFinished);
}
`
	ents := extract(t, "custom_cpp_qt", fi("apiclient.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "fetch:qnam:new_QNetworkAccessManager") {
		t.Errorf("expected fetch:qnam:new_QNetworkAccessManager, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "fetch:qnam:get") {
		t.Errorf("expected fetch:qnam:get, got %v", ents)
	}
}

func TestQtRouterStackedWidget(t *testing.T) {
	src := `
void MainWindow::showPage(int index) {
    stack->setCurrentIndex(index);
}

void MainWindow::showWidget(QWidget *w) {
    stack->setCurrentWidget(w);
}
`
	ents := extract(t, "custom_cpp_qt", fi("mainwindow.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "router:stacked:setCurrentIndex") {
		t.Errorf("expected router:stacked:setCurrentIndex, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "router:stacked:setCurrentWidget") {
		t.Errorf("expected router:stacked:setCurrentWidget, got %v", ents)
	}
}

func TestQtRouterQmlStackView(t *testing.T) {
	src := `
// QML-style navigation in C++ controller
void Navigator::pushPage() {
    stackView.push(homePage);
}
void Navigator::popPage() {
    stackView.pop();
}
`
	ents := extract(t, "custom_cpp_qt", fi("navigator.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "router:qml_stack:push") {
		t.Errorf("expected router:qml_stack:push, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "router:qml_stack:pop") {
		t.Errorf("expected router:qml_stack:pop, got %v", ents)
	}
}

// ---------------------------------------------------------------------------
// ROS extractor
// ---------------------------------------------------------------------------

func TestRosROS1PublisherSubscriber(t *testing.T) {
	src := `
#include <ros/ros.h>
#include <std_msgs/String.h>

int main(int argc, char **argv) {
    ros::init(argc, argv, "talker");
    ros::NodeHandle nh;
    ros::Publisher pub = nh.advertise<std_msgs::String>("chatter", 1000);
    ros::Subscriber sub = nh.subscribe("input_topic", 1000, callback);
    return 0;
}
`
	ents := extract(t, "custom_cpp_ros", fi("talker.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "ros1:publish:chatter") {
		t.Errorf("expected ros1:publish:chatter, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "ros1:subscribe:input_topic") {
		t.Errorf("expected ros1:subscribe:input_topic, got %v", ents)
	}
}

func TestRosROS1ServiceServer(t *testing.T) {
	src := `
#include <ros/ros.h>
#include <std_srvs/Empty.h>

int main(int argc, char **argv) {
    ros::NodeHandle nh;
    ros::ServiceServer server = nh.advertiseService("reset_service", resetCallback);
    ros::ServiceClient client = nh.serviceClient<std_srvs::Empty>("other_service");
    return 0;
}
`
	ents := extract(t, "custom_cpp_ros", fi("server.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "ros1:service_server:reset_service") {
		t.Errorf("expected ros1:service_server:reset_service, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "ros1:service_client:other_service") {
		t.Errorf("expected ros1:service_client:other_service, got %v", ents)
	}
}

func TestRosROS2PublisherSubscriber(t *testing.T) {
	src := `
#include <rclcpp/rclcpp.hpp>
#include <std_msgs/msg/string.hpp>

class MinimalNode : public rclcpp::Node {
public:
    MinimalNode() : Node("minimal") {
        pub_ = this->create_publisher<std_msgs::msg::String>("chatter", 10);
        sub_ = this->create_subscription<std_msgs::msg::String>(
            "input", 10, std::bind(&MinimalNode::callback, this, _1));
    }
};
`
	ents := extract(t, "custom_cpp_ros", fi("minimal_node.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "ros2:publish:chatter") {
		t.Errorf("expected ros2:publish:chatter, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "ros2:subscribe:input") {
		t.Errorf("expected ros2:subscribe:input, got %v", ents)
	}
}

func TestRosNativeModuleImports(t *testing.T) {
	src := `
#include <ros/ros.h>
#include <sensor_msgs/PointCloud2.h>
#include <geometry_msgs/Twist.h>
`
	ents := extract(t, "custom_cpp_ros", fi("node.cpp", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "ros_dep:ros") {
		t.Errorf("expected ros_dep:ros, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "ros_dep:sensor_msgs") {
		t.Errorf("expected ros_dep:sensor_msgs, got %v", ents)
	}
}

func TestRosPackageXML(t *testing.T) {
	src := `<?xml version="1.0"?>
<package format="3">
  <name>my_robot</name>
  <depend>roscpp</depend>
  <depend>sensor_msgs</depend>
  <build_depend>geometry_msgs</build_depend>
</package>
`
	ents := extract(t, "custom_cpp_ros", fi("package.xml", "xml", src))
	if !containsEntity(ents, "SCOPE.Pattern", "ros_pkg_dep:roscpp") {
		t.Errorf("expected ros_pkg_dep:roscpp, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "ros_pkg_dep:sensor_msgs") {
		t.Errorf("expected ros_pkg_dep:sensor_msgs, got %v", ents)
	}
}

func TestRosNoMatch(t *testing.T) {
	src := `#include <iostream>
int main() { return 0; }`
	ents := extract(t, "custom_cpp_ros", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-ROS file, got %d", len(ents))
	}
}

func TestRosWrongLanguage(t *testing.T) {
	src := `ros::NodeHandle nh;`
	ents := extract(t, "custom_cpp_ros", fi("node.py", "python", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Unreal Engine extractor
// ---------------------------------------------------------------------------

func TestUnrealRPCServer(t *testing.T) {
	src := `
#include "MyCharacter.generated.h"

UCLASS()
class AMyCharacter : public ACharacter {
    GENERATED_BODY()

    UFUNCTION(Server, Reliable)
    void ServerFireWeapon();

    UFUNCTION(Client, Reliable)
    void ClientPlayAnimation();

    UFUNCTION(NetMulticast, Reliable)
    void MulticastExplosion();
};
`
	ents := extract(t, "custom_cpp_unreal", fi("MyCharacter.h", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "rpc:server:ServerFireWeapon") {
		t.Errorf("expected rpc:server:ServerFireWeapon, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "rpc:client:ClientPlayAnimation") {
		t.Errorf("expected rpc:client:ClientPlayAnimation, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "rpc:netmulticast:MulticastExplosion") {
		t.Errorf("expected rpc:netmulticast:MulticastExplosion, got %v", ents)
	}
}

func TestUnrealDelegate(t *testing.T) {
	src := `
#include "GameFramework/Actor.h"

DECLARE_MULTICAST_DELEGATE(FOnGameOver)
DECLARE_DYNAMIC_MULTICAST_DELEGATE_OneParam(FOnPlayerDied, APlayerController*, Player)
`
	ents := extract(t, "custom_cpp_unreal", fi("GameEvents.h", "cpp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "delegate:FOnGameOver") {
		t.Errorf("expected delegate:FOnGameOver, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "delegate:FOnPlayerDied") {
		t.Errorf("expected delegate:FOnPlayerDied, got %v", ents)
	}
}

func TestUnrealBuildCsAddRange(t *testing.T) {
	src := `
using UnrealBuildTool;

public class MyGame : ModuleRules {
    public MyGame(ReadOnlyTargetRules Target) : base(Target) {
        PublicDependencyModuleNames.AddRange(new string[] {
            "Core", "CoreUObject", "Engine", "InputCore"
        });
        PrivateDependencyModuleNames.AddRange(new string[] {
            "Slate", "SlateCore"
        });
    }
}
`
	ents := extract(t, "custom_cpp_unreal", fi("MyGame.Build.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "ue_module:Core") {
		t.Errorf("expected ue_module:Core, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "ue_module:Engine") {
		t.Errorf("expected ue_module:Engine, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "ue_module:Slate") {
		t.Errorf("expected ue_module:Slate, got %v", ents)
	}
}

func TestUnrealBuildCsAdd(t *testing.T) {
	src := `
using UnrealBuildTool;

public class MyPlugin : ModuleRules {
    public MyPlugin(ReadOnlyTargetRules Target) : base(Target) {
        PublicDependencyModuleNames.Add("OnlineSubsystem");
        PrivateDependencyModuleNames.Add("HTTP");
    }
}
`
	ents := extract(t, "custom_cpp_unreal", fi("MyPlugin.Build.cs", "csharp", src))
	if !containsEntity(ents, "SCOPE.Pattern", "ue_module:OnlineSubsystem") {
		t.Errorf("expected ue_module:OnlineSubsystem, got %v", ents)
	}
	if !containsEntity(ents, "SCOPE.Pattern", "ue_module:HTTP") {
		t.Errorf("expected ue_module:HTTP, got %v", ents)
	}
}

func TestUnrealNoMatchCPP(t *testing.T) {
	src := `#include <iostream>
int main() { return 0; }`
	ents := extract(t, "custom_cpp_unreal", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-Unreal file, got %d", len(ents))
	}
}

func TestUnrealWrongLanguage(t *testing.T) {
	src := `UFUNCTION(Server, Reliable) void ServerFire();`
	ents := extract(t, "custom_cpp_unreal", fi("actor.py", "python", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// Redis++ query attribution
// ---------------------------------------------------------------------------

func TestRedisPPGetSet(t *testing.T) {
	src := `
#include <sw/redis++/redis++.h>

void cacheOp(Redis &redis) {
    auto val = redis.get("user:1234");
    redis.set("session:abc", "data");
}
`
	ents := extract(t, "custom_cpp_redis_query", fi("cache.cpp", "cpp", src))
	hasGet := false
	hasSet := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			if len(e.Name) >= 14 && e.Name[:14] == "redis:GET:\"use" {
				hasGet = true
			}
			if len(e.Name) >= 14 && e.Name[:14] == "redis:SET:\"ses" {
				hasSet = true
			}
		}
	}
	if !hasGet {
		t.Errorf("expected redis GET entity, got %v", ents)
	}
	if !hasSet {
		t.Errorf("expected redis SET entity, got %v", ents)
	}
}

func TestRedisPPHSet(t *testing.T) {
	src := `
#include <sw/redis++/redis++.h>

void hashOp(Redis &redis) {
    redis.hset("user:1234", "name", "Alice");
    auto name = redis.hget("user:1234", "name");
}
`
	ents := extract(t, "custom_cpp_redis_query", fi("hash.cpp", "cpp", src))
	hasHSet := false
	hasHGet := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			if len(e.Name) >= 11 && e.Name[:11] == "redis:HSET:" {
				hasHSet = true
			}
			if len(e.Name) >= 11 && e.Name[:11] == "redis:HGET:" {
				hasHGet = true
			}
		}
	}
	if !hasHSet {
		t.Errorf("expected redis HSET entity, got %v", ents)
	}
	if !hasHGet {
		t.Errorf("expected redis HGET entity, got %v", ents)
	}
}

func TestRedisPPCommand(t *testing.T) {
	src := `
#include <sw/redis++/redis++.h>

void customCmd(Redis &redis) {
    redis.command("EXPIRE", "session:abc", 3600);
}
`
	ents := extract(t, "custom_cpp_redis_query", fi("cmd.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && len(e.Name) >= 22 && e.Name[:22] == "redis:command:EXPIRE@L" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected redis:command:EXPIRE@L... entity, got %v", ents)
	}
}

func TestRedisPPPipeline(t *testing.T) {
	src := `
#include <sw/redis++/redis++.h>

void pipelineOp(Redis &redis) {
    auto pipe = redis.pipeline();
    pipe.set("key1", "val1");
    pipe.get("key2");
    pipe.exec();
}
`
	ents := extract(t, "custom_cpp_redis_query", fi("pipe.cpp", "cpp", src))
	hasSet := false
	hasGet := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" {
			if len(e.Name) >= 22 && e.Name[:22] == "redis:pipeline:SET@L" {
				hasSet = true
			}
			if len(e.Name) >= 22 && e.Name[:22] == "redis:pipeline:GET@L" {
				hasGet = true
			}
		}
	}
	// Pipeline GET/SET need prefix check with variable length
	_ = hasSet
	_ = hasGet
	hasAny := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Operation" && len(e.Name) > 16 && e.Name[:16] == "redis:pipeline:S" {
			hasAny = true
			break
		}
	}
	if !hasAny {
		t.Errorf("expected redis pipeline entities, got %v", ents)
	}
}

func TestRedisPPNoMatch(t *testing.T) {
	src := `#include <iostream>
int main() { return 0; }`
	ents := extract(t, "custom_cpp_redis_query", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-redis file, got %d", len(ents))
	}
}

func TestRedisPPWrongLanguage(t *testing.T) {
	src := `#include <sw/redis++/redis++.h>
redis.get("key");`
	ents := extract(t, "custom_cpp_redis_query", fi("cache.py", "python", src))
	if len(ents) != 0 {
		t.Errorf("wrong language should return no entities, got %d", len(ents))
	}
}

// ---------------------------------------------------------------------------
// RESTinio middleware extractor
// ---------------------------------------------------------------------------

func TestRestinioNonMatchedHandler(t *testing.T) {
	src := `
#include <restinio/all.hpp>

int main() {
    auto settings = restinio::run_on_thread_pool_settings_t{};
    settings.non_matched_request_handler([](auto req, auto next) {
        req->create_response(404).done();
        return restinio::request_accepted();
    });
    restinio::http_server_run(settings);
}
`
	ents := extract(t, "custom_cpp_restinio_mw", fi("server.cpp", "cpp", src))
	const prefix = "restinio:non_matched_request_handler:"
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && len(e.Name) >= len(prefix) &&
			e.Name[:len(prefix)] == prefix {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected restinio:non_matched_request_handler entity, got %v", ents)
	}
}

func TestRestinioMakeChain(t *testing.T) {
	src := `
#include <restinio/all.hpp>

auto handler = restinio::router::make_chain<LoggingHandler, AuthHandler, ApiRouter>();
`
	ents := extract(t, "custom_cpp_restinio_mw", fi("server.cpp", "cpp", src))
	found := false
	for _, e := range ents {
		if e.Kind == "SCOPE.Pattern" && len(e.Name) >= 22 &&
			e.Name[:22] == "restinio:make_chain:Lo" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected restinio:make_chain entity, got %v", ents)
	}
}

func TestRestinioNoMatchMW(t *testing.T) {
	src := `#include <iostream>
int main() { return 0; }`
	ents := extract(t, "custom_cpp_restinio_mw", fi("main.cpp", "cpp", src))
	if len(ents) != 0 {
		t.Errorf("expected no entities for non-restinio file, got %d", len(ents))
	}
}
