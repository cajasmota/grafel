package cpp

// ros_extractor.go — ROS (Robot Operating System) C++ extractor.
//
// Covered DSL surfaces (partial — heuristic regex; no AST):
//
//  ipc_extraction:       ros::Publisher / ros::Subscriber / ros::ServiceServer /
//                        ros::ServiceClient / ros::ActionServer patterns.
//                        ROS2: rclcpp::Publisher / rclcpp::Subscription /
//                        rclcpp::Service / rclcpp::Client.
//                        Also: advertise(), subscribe(), advertiseService(),
//                        serviceClient() calls.
//
//  native_module_imports: #include <ros/...> / #include <rclcpp/...> headers
//                         and package.xml <depend>...</depend> entries (treated
//                         as native module imports).
//
//  main_renderer_split:  not_applicable — ROS has no main/renderer process split
//                        (it is a robotics middleware, not a UI framework).
//
// Status: partial

import (
	"context"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/cajasmota/archigraph/internal/extractor"
	"github.com/cajasmota/archigraph/internal/types"
)

func init() {
	extractor.Register("custom_cpp_ros", &rosExtractor{})
}

type rosExtractor struct{}

func (e *rosExtractor) Language() string { return "custom_cpp_ros" }

var (
	// Gate: must look like a ROS file.
	reRosInclude = regexp.MustCompile(`#\s*include\s+[<"](ros/|rclcpp/|rclcpp_action/|rcl/|sensor_msgs/|std_msgs/|geometry_msgs/|nav_msgs/)`)

	// ROS1: nh.advertise<T>("topic", ...)
	reRos1Advertise = regexp.MustCompile(`\b(?:\w+)\s*\.\s*advertise\s*<[^>]*>\s*\(\s*"([^"]+)"`)
	// ROS1: nh.subscribe("topic", ..., &callback)
	reRos1Subscribe = regexp.MustCompile(`\b(?:\w+)\s*\.\s*subscribe\s*\(\s*"([^"]+)"`)
	// ROS1: nh.advertiseService("service_name", ...)
	reRos1AdvertiseService = regexp.MustCompile(`\b(?:\w+)\s*\.\s*advertiseService\s*\(\s*"([^"]+)"`)
	// ROS1: nh.serviceClient<T>("service_name")
	reRos1ServiceClient = regexp.MustCompile(`\b(?:\w+)\s*\.\s*serviceClient\s*(?:<[^>]*>)?\s*\(\s*"([^"]+)"`)

	// ROS2: this->create_publisher<T>("topic", ...)
	reRos2CreatePub = regexp.MustCompile(`\bcreate_publisher\s*<[^>]*>\s*\(\s*"([^"]+)"`)
	// ROS2: this->create_subscription<T>("topic", ...)
	reRos2CreateSub = regexp.MustCompile(`\bcreate_subscription\s*<[^>]*>\s*\(\s*"([^"]+)"`)
	// ROS2: this->create_service<T>("service_name", ...)
	reRos2CreateService = regexp.MustCompile(`\bcreate_service\s*<[^>]*>\s*\(\s*"([^"]+)"`)
	// ROS2: this->create_client<T>("service_name")
	reRos2CreateClient = regexp.MustCompile(`\bcreate_client\s*<[^>]*>\s*\(\s*"([^"]+)"`)

	// ROS package.xml dependency: <depend>pkg_name</depend> or <build_depend>
	reRosPkgXmlDepend = regexp.MustCompile(`<(?:depend|build_depend|run_depend|exec_depend)>\s*([A-Za-z_][A-Za-z0-9_]*)\s*</`)

	// ROS #include headers → native module imports
	reRosIncludeHeader = regexp.MustCompile(`#\s*include\s+[<"]((?:ros|rclcpp|rclcpp_action|rcl|sensor_msgs|std_msgs|geometry_msgs|nav_msgs|tf2|actionlib)/[^>"]+)[>"]`)
)

func (e *rosExtractor) Extract(ctx context.Context, file extractor.FileInput) ([]types.EntityRecord, error) {
	tracer := otel.Tracer("archigraph/custom/cpp")
	_, span := tracer.Start(ctx, "indexer.ros_extractor.extract",
		trace.WithAttributes(
			attribute.String("language", file.Language),
			attribute.String("framework", "ros"),
			attribute.String("file_path", file.Path),
		),
	)
	defer span.End()

	if len(file.Content) == 0 {
		return nil, nil
	}

	src := string(file.Content)

	// Support both .cpp/.h files (language=cpp) and package.xml (language=xml or path ends .xml)
	isXML := strings.HasSuffix(file.Path, "package.xml") ||
		strings.HasSuffix(file.Path, ".xml") ||
		file.Language == "xml"
	isCPP := file.Language == "cpp" || file.Language == "c"

	if !isCPP && !isXML {
		return nil, nil
	}

	// For C++ files: gate on ROS include markers.
	if isCPP && !reRosInclude.MatchString(src) {
		return nil, nil
	}

	var entities []types.EntityRecord
	seen := make(map[string]bool)

	add := func(ent types.EntityRecord) {
		key := ent.Kind + ":" + ent.Name
		if seen[key] {
			return
		}
		seen[key] = true
		entities = append(entities, ent)
	}

	if isCPP {
		// --- ipc_extraction ---
		// ROS1 publishers
		for _, m := range reRos1Advertise.FindAllStringSubmatchIndex(src, -1) {
			topic := src[m[2]:m[3]]
			name := "ros1:publish:" + topic
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "ros", "provenance", "INFERRED_FROM_ROS1_ADVERTISE",
				"ipc_kind", "publisher", "topic", topic)
			add(ent)
		}
		// ROS1 subscribers
		for _, m := range reRos1Subscribe.FindAllStringSubmatchIndex(src, -1) {
			topic := src[m[2]:m[3]]
			name := "ros1:subscribe:" + topic
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "ros", "provenance", "INFERRED_FROM_ROS1_SUBSCRIBE",
				"ipc_kind", "subscriber", "topic", topic)
			add(ent)
		}
		// ROS1 service servers
		for _, m := range reRos1AdvertiseService.FindAllStringSubmatchIndex(src, -1) {
			svc := src[m[2]:m[3]]
			name := "ros1:service_server:" + svc
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "ros", "provenance", "INFERRED_FROM_ROS1_ADVERTISE_SERVICE",
				"ipc_kind", "service_server", "service", svc)
			add(ent)
		}
		// ROS1 service clients
		for _, m := range reRos1ServiceClient.FindAllStringSubmatchIndex(src, -1) {
			svc := src[m[2]:m[3]]
			name := "ros1:service_client:" + svc
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "ros", "provenance", "INFERRED_FROM_ROS1_SERVICE_CLIENT",
				"ipc_kind", "service_client", "service", svc)
			add(ent)
		}
		// ROS2 publishers
		for _, m := range reRos2CreatePub.FindAllStringSubmatchIndex(src, -1) {
			topic := src[m[2]:m[3]]
			name := "ros2:publish:" + topic
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "ros", "provenance", "INFERRED_FROM_ROS2_CREATE_PUBLISHER",
				"ipc_kind", "publisher", "topic", topic)
			add(ent)
		}
		// ROS2 subscriptions
		for _, m := range reRos2CreateSub.FindAllStringSubmatchIndex(src, -1) {
			topic := src[m[2]:m[3]]
			name := "ros2:subscribe:" + topic
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "ros", "provenance", "INFERRED_FROM_ROS2_CREATE_SUBSCRIPTION",
				"ipc_kind", "subscriber", "topic", topic)
			add(ent)
		}
		// ROS2 service servers
		for _, m := range reRos2CreateService.FindAllStringSubmatchIndex(src, -1) {
			svc := src[m[2]:m[3]]
			name := "ros2:service_server:" + svc
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "ros", "provenance", "INFERRED_FROM_ROS2_CREATE_SERVICE",
				"ipc_kind", "service_server", "service", svc)
			add(ent)
		}
		// ROS2 service clients
		for _, m := range reRos2CreateClient.FindAllStringSubmatchIndex(src, -1) {
			svc := src[m[2]:m[3]]
			name := "ros2:service_client:" + svc
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "ros", "provenance", "INFERRED_FROM_ROS2_CREATE_CLIENT",
				"ipc_kind", "service_client", "service", svc)
			add(ent)
		}

		// --- native_module_imports: #include <ros/...> headers ---
		seen2 := map[string]bool{}
		for _, m := range reRosIncludeHeader.FindAllStringSubmatchIndex(src, -1) {
			header := src[m[2]:m[3]]
			// derive module name: e.g. "ros/ros.h" -> "ros", "sensor_msgs/PointCloud2.h" -> "sensor_msgs"
			parts := strings.SplitN(header, "/", 2)
			modName := parts[0]
			if seen2[modName] {
				continue
			}
			seen2[modName] = true
			name := "ros_dep:" + modName
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, file.Language, lineOf(src, m[0]))
			setProps(&ent, "framework", "ros", "provenance", "INFERRED_FROM_ROS_INCLUDE",
				"module_kind", "native_import", "module_name", modName)
			add(ent)
		}
	}

	if isXML {
		// package.xml: <depend>pkg_name</depend> -> native module imports
		seen2 := map[string]bool{}
		for _, m := range reRosPkgXmlDepend.FindAllStringSubmatchIndex(src, -1) {
			pkg := src[m[2]:m[3]]
			if seen2[pkg] {
				continue
			}
			seen2[pkg] = true
			name := "ros_pkg_dep:" + pkg
			ent := makeEntity(name, "SCOPE.Pattern", "", file.Path, "xml", lineOf(src, m[0]))
			setProps(&ent, "framework", "ros", "provenance", "INFERRED_FROM_ROS_PACKAGE_XML",
				"module_kind", "ros_package_dep", "module_name", pkg)
			add(ent)
		}
	}

	span.SetAttributes(attribute.Int("entity_count", len(entities)))
	return entities, nil
}
