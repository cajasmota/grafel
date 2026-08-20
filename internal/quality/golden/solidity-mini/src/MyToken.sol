// SPDX-License-Identifier: MIT
// Hand-written for the grafel solidity-mini fixture (see NOTICE.md).
//
// This file exists for ONE shape: base-constructor arguments in the `is`
// clause. It is the canonical OpenZeppelin usage — the form the OZ ERC20
// documentation shows for naming a token — and #6423 claims contractRE cannot
// match it, which would delete the contract AND every member below it.
//
// Everything in here is therefore expected and, at the time this fixture
// landed, none of it was produced. Nothing else in the fixture depends on this
// file, so its recall number is readable on its own.

pragma solidity ^0.8.20;

import {ERC20} from "@openzeppelin/contracts/token/ERC20/ERC20.sol";

contract MyToken is ERC20("My Token", "MTK") {
    uint256 public cap;

    event Minted(address indexed to, uint256 amount);

    constructor(uint256 initialCap) {
        cap = initialCap;
    }

    function mint(address to, uint256 amount) public {
        cap = cap - amount;
        _mint(to, amount);
        emit Minted(to, amount);
    }
}
