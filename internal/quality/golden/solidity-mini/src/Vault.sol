// SPDX-License-Identifier: MIT
// Hand-written for the grafel solidity-mini fixture (see NOTICE.md).
//
// Carries the shapes that no OpenZeppelin file in this fixture happens to
// contain, each modelled on ordinary production Solidity:
//
//   * a file-level (free) function, struct and enum        (#6423 — no code path)
//   * receive() and fallback()                             (#6423 — denylisted)
//   * a modifier USAGE in a function signature             (#6423 — declBody skips it)
//   * a Yul `function` inside assembly{}                   (#6425 — phantom operation)
//   * `type(uint256).max` and `abi.encode(...)`            (#6425 — CALLS over-fire)
//   * an uppercase bare call `IERC20(...)`                 (#6425 — CALLS under-fire)
//
// and ordinary shapes that the extractor already handles — a plain `is` clause,
// state variables, an event, a modifier declaration, dotted and bare calls — so
// the fixture is not uniformly red and a regression in the working half is
// still a failure.

pragma solidity ^0.8.20;

import {Ownable} from "./Ownable.sol";

error VaultLocked(address caller);

enum VaultState {
    Open,
    Locked
}

struct Deposit {
    address owner;
    uint256 amount;
}

function computeFee(uint256 amount) pure returns (uint256) {
    return amount / 100;
}

contract Vault is Ownable {
    uint256 public total;
    VaultState public state;
    mapping(address => Deposit) private _deposits;

    event Deposited(address indexed from, uint256 amount);

    error VaultEmpty();

    struct Receipt {
        bytes32 id;
        uint256 amount;
    }

    enum Tier {
        Basic,
        Premium
    }

    constructor(address initialOwner) Ownable(initialOwner) {
        state = VaultState.Open;
    }

    receive() external payable {
        total = total + msg.value;
    }

    fallback() external payable {
        revert VaultLocked(msg.sender);
    }

    modifier whenOpen() {
        if (state == VaultState.Locked) {
            revert VaultLocked(msg.sender);
        }
        _;
    }

    function deposit(uint256 amount) public whenOpen {
        uint256 fee = computeFee(amount);
        total = total + amount - fee;
        emit Deposited(msg.sender, amount);
    }

    function lock() public onlyOwner {
        state = VaultState.Locked;
    }

    function ceiling() public pure returns (uint256) {
        return type(uint256).max;
    }

    function fingerprint(uint256 amount) public view returns (bytes32) {
        return keccak256(abi.encode(address(this), amount));
    }

    function sweep(address token, address to) public onlyOwner {
        IERC20(token).transfer(to, total);
        total = 0;
    }

    function roundUp(uint256 amount) public pure returns (uint256) {
        uint256 out;
        assembly {
            function helper(x) -> y {
                y := add(x, 1)
            }
            out := helper(amount)
        }
        return out;
    }
}

interface IERC20 {
    function transfer(address to, uint256 amount) external returns (bool);
}
